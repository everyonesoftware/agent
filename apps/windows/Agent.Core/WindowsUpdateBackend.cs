using System.Text.Json;
using System.Text.Json.Serialization;
using Velopack;
using Velopack.Logging;
using Velopack.Sources;

namespace Agent.Core;

/// <summary>Velopack 1.2.161 GithubSource only reads ten releases. Page explicitly and isolate the Windows feed.</summary>
public sealed class WindowsGithubSource : GithubSource
{
    public const string Repository = "https://github.com/everyonesoftware/agent";
    public const string Channel = "win-x64";
    private readonly Dictionary<GithubRelease, string> versions = new();

    public WindowsGithubSource(IFileDownloader? downloader = null) : base(Repository, accessToken: null, prerelease: true, downloader: downloader) { }

    protected override async Task<GithubRelease[]> GetReleases(bool includePrereleases)
    {
        versions.Clear();
        var result = new List<(GithubRelease Release, Version Version, bool MainRelease)>();
        // Bound API use; never silently call a truncated inventory "up to date".
        for (int page = 1; page <= 10; page++)
        {
            string json = await Downloader.DownloadString($"https://api.github.com/repos/everyonesoftware/agent/releases?per_page=100&page={page}", GetRequestHeaders("application/vnd.github+json"));
            using var document = JsonDocument.Parse(json);
            var releases = document.RootElement.EnumerateArray().ToArray();
            foreach (var item in releases)
            {
                if (item.GetProperty("draft").GetBoolean()) continue;
                string? tag = item.GetProperty("tag_name").GetString();
                bool mainRelease = tag is not null && tag.StartsWith("v", StringComparison.Ordinal) && !item.GetProperty("prerelease").GetBoolean();
                if (!mainRelease) continue;
                string version = tag![1..];
                if (!Version.TryParse(version, out var parsed) || parsed.Major != 0 || parsed.Minor != 1 || parsed.Build is < 1 or > 65534 || parsed.Revision != -1 || version != parsed.ToString()) continue;
                var release = item.Deserialize(WindowsUpdateJsonContext.Default.GithubRelease) ?? throw new InvalidDataException("Missing Windows release.");
                if (!release.Assets.Any(a => a.Name == $"releases.{Channel}.json")) continue;
                // A release's metadata must not redirect us to another tag, repository, or platform.
                string prefix = $"{Repository}/releases/download/{tag}/";
                if (release.Assets.Any(a => a.BrowserDownloadUrl != prefix + a.Name))
                    throw new InvalidDataException("Unexpected Windows release asset origin.");
                versions.Add(release, version);
                result.Add((release, parsed, mainRelease));
            }
            // Releases are not guaranteed to be published in numeric version order. Deltas are disabled,
            // so only the highest Windows full-package feed is needed, not every historical feed.
            if (releases.Length < 100)
            {
                var selected = result.OrderByDescending(r => r.Version).ThenByDescending(r => r.MainRelease).Take(1).Select(r => r.Release).ToArray();
                if (selected.Any(r => r.Assets.GroupBy(a => a.Name, StringComparer.OrdinalIgnoreCase).Any(g => g.Count() != 1)))
                    throw new InvalidDataException("Ambiguous Windows release assets.");
                return selected;
            }
        }
        throw new InvalidDataException("GitHub release inventory exceeded the safe paging limit.");
    }

    public override async Task<VelopackAssetFeed> GetReleaseFeed(IVelopackLogger logger, string? appId, string channel, Guid? stagingId = null, VelopackAsset? latestLocalRelease = null)
    {
        if (channel != Channel || appId is not null && appId != "AgentWindows")
            throw new InvalidDataException("Unexpected Windows update identity.");
        var feed = await base.GetReleaseFeed(logger, appId, channel, stagingId, latestLocalRelease);
        var safe = new List<VelopackAsset>();
        foreach (var asset in feed.Assets)
        {
            if (asset.PackageId != "AgentWindows") continue;
            if (asset is not GitBaseAsset entry || !versions.TryGetValue(entry.Release, out string? version) || asset.Version.ToString() != version || asset.Type != VelopackAssetType.Full || asset.FileName != $"AgentWindows-{version}-{Channel}-full.nupkg")
                throw new InvalidDataException("Unexpected Windows update package.");
            if (!entry.Release.Assets.Any(a => a.Name == asset.FileName))
                throw new InvalidDataException("Missing Windows update package.");
            safe.Add(asset);
        }
        if (versions.Count > 0 && safe.Count != 1)
            throw new InvalidDataException("Windows release must contain exactly one matching full package.");
        return new VelopackAssetFeed { Assets = safe.ToArray() };
    }
}

[JsonSerializable(typeof(GithubRelease))]
internal partial class WindowsUpdateJsonContext : JsonSerializerContext { }

/// <summary>Lazy construction keeps native locator work out of startup. Download never implies apply.</summary>
public sealed class WindowsUpdateBackend : IWindowsUpdateBackend
{
    private readonly Lazy<UpdateManager> manager = new(() => new UpdateManager(new WindowsGithubSource(), new UpdateOptions
    {
        ExplicitChannel = WindowsGithubSource.Channel,
        AllowVersionDowngrade = false,
        MaximumDeltasBeforeFallback = -1
    }));
    private UpdateInfo? offered;
    private bool downloaded;
    public bool IsInstalled => manager.Value.IsInstalled;
    public Version CurrentVersion => Version.Parse(manager.Value.CurrentVersion?.ToString() ?? "0.0.0");

    public async Task<Version?> CheckAsync()
    {
        offered = null;
        downloaded = false;
        var update = await manager.Value.CheckForUpdatesAsync();
        if (update is null) return null;
        var version = Version.Parse(update.TargetFullRelease.Version.ToString());
        if (version <= CurrentVersion || update.IsDowngrade || update.TargetFullRelease.PackageId != "AgentWindows")
            throw new InvalidDataException("Unsafe Windows update target.");
        offered = update;
        return version;
    }

    private UpdateInfo RequireOffer(Version version)
    {
        if (offered is null || offered.TargetFullRelease.Version.ToString() != version.ToString() || version <= CurrentVersion)
            throw new InvalidOperationException("No matching newer Windows update.");
        return offered;
    }

    public async Task DownloadAsync(Version version, Action<int> progress, CancellationToken token)
    {
        downloaded = false;
        await manager.Value.DownloadUpdatesAsync(RequireOffer(version), progress, token);
        token.ThrowIfCancellationRequested();
        downloaded = true;
    }

    public void ApplyAndRestart(Version version)
    {
        var update = RequireOffer(version);
        if (!downloaded) throw new InvalidOperationException("Download the Windows update first.");
        manager.Value.ApplyUpdatesAndRestart(update.TargetFullRelease, Array.Empty<string>());
    }
}
