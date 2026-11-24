using System.Text.Json;
using System.Text.Json.Serialization;
using System.Runtime.CompilerServices;
using Velopack;
using Velopack.Exceptions;
using Velopack.Locators;
using Velopack.Logging;
using Velopack.Sources;

[assembly: InternalsVisibleTo("Agent.Core.Tests")]

namespace Agent.Core;

/// <summary>Velopack 1.2.161 GithubSource only reads ten releases. Page explicitly and isolate the Windows feed.</summary>
public sealed class WindowsGithubSource : GithubSource
{
    public const string Repository = "https://github.com/everyonesoftware/agent";
    public const string Channel = "win-x64";
    private readonly Dictionary<GithubRelease, string> versions = new();
    private readonly HashSet<GithubRelease> releasesWithManifest = new();

    public WindowsGithubSource(IFileDownloader? downloader = null) : base(Repository, accessToken: null, prerelease: true,
        downloader: downloader ?? new WindowsDiscoveryDownloader(WindowsDiscoveryDownloader.PublicHttp, DefaultCooldownPath(), TimeProvider.System)) { }

    internal WindowsGithubSource(HttpClient discoveryHttp, string? cooldownPath, TimeProvider clock)
        : this(new WindowsDiscoveryDownloader(discoveryHttp, cooldownPath, clock)) { }

    private static string? DefaultCooldownPath()
    {
        string localData = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData, Environment.SpecialFolderOption.DoNotVerify);
        return string.IsNullOrEmpty(localData) ? null : Path.Combine(localData, "EveryoneSoftware", "Agent", "windows-update-cooldown");
    }

    internal bool IsStartupCheckDeferred => Downloader is WindowsDiscoveryDownloader { IsStartupCheckDeferred: true };

    protected override async Task<GithubRelease[]> GetReleases(bool includePrereleases)
    {
        versions.Clear();
        releasesWithManifest.Clear();
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
                // A release's metadata must not redirect us to another tag, repository, or platform.
                string prefix = $"{Repository}/releases/download/{tag}/";
                var assets = new List<GithubReleaseAsset>();
                int manifestAssets = 0;
                bool validManifestAsset = false;
                foreach (var asset in item.GetProperty("assets").EnumerateArray())
                {
                    var names = asset.EnumerateObject().Where(p => p.NameEquals("name")).Select(p => p.Value).ToArray();
                    if (names.Length > 0 && names.All(n => n.ValueKind == JsonValueKind.String &&
                        EmbeddedWindowsNotes.ManifestName.Equals(n.GetString(), StringComparison.OrdinalIgnoreCase)))
                    {
                        // Manifest metadata is optional. Do not pass malformed notes fields to Velopack's parser.
                        manifestAssets++;
                        var urls = asset.EnumerateObject().Where(p => p.NameEquals("browser_download_url")).Select(p => p.Value).ToArray();
                        validManifestAsset = names.Length == 1 && names[0].GetString() == EmbeddedWindowsNotes.ManifestName &&
                            urls is [{ ValueKind: JsonValueKind.String } url] && url.GetString() == prefix + EmbeddedWindowsNotes.ManifestName;
                        continue;
                    }
                    if (names.Length != 1) throw new InvalidDataException("Missing or ambiguous Windows release asset name.");
                    assets.Add(asset.Deserialize(WindowsUpdateJsonContext.Default.GithubReleaseAsset) ?? throw new InvalidDataException("Missing Windows release asset."));
                }
                if (!assets.Any(a => a.Name == $"releases.{Channel}.json")) continue;
                var release = new GithubRelease { Assets = assets.ToArray() };
                if (release.Assets.Any(a => a.BrowserDownloadUrl != prefix + a.Name))
                    throw new InvalidDataException("Unexpected Windows release asset origin.");
                if (manifestAssets == 1 && validManifestAsset) releasesWithManifest.Add(release);
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

    internal bool HasManifest(VelopackAsset offer) => offer is GitBaseAsset entry && releasesWithManifest.Contains(entry.Release);
}

[JsonSerializable(typeof(GithubReleaseAsset))]
internal partial class WindowsUpdateJsonContext : JsonSerializerContext { }

/// <summary>Lazy construction keeps native locator work out of startup. Download never implies apply.</summary>
public sealed class WindowsUpdateBackend : IWindowsUpdateBackend
{
    private readonly Lazy<VerificationUpdateManager> manager;
    private readonly WindowsGithubSource source;
    private readonly string? informationalVersion;
    private readonly HttpClient? notesTransport;

    public WindowsUpdateBackend(string? informationalVersion = null) : this(informationalVersion, new WindowsGithubSource(), null, null) { }

    internal WindowsUpdateBackend(string? informationalVersion, WindowsGithubSource source, IVelopackLocator? locator, HttpClient? transport)
    {
        this.informationalVersion = informationalVersion;
        this.source = source;
        notesTransport = transport;
        manager = new(() => new VerificationUpdateManager(source, new UpdateOptions
        {
            ExplicitChannel = WindowsGithubSource.Channel,
            AllowVersionDowngrade = false,
            MaximumDeltasBeforeFallback = -1
        }, locator));
    }
    private UpdateInfo? offered;
    private bool downloaded;
    public bool IsInstalled => manager.Value.IsInstalled;
    public bool IsStartupCheckDeferred => source.IsStartupCheckDeferred;
    public Version CurrentVersion => Version.Parse(manager.Value.CurrentVersion?.ToString() ?? "0.0.0");
    public ReleaseNotes? OfferedNotes { get; private set; }

    public async Task<Version?> CheckAsync()
    {
        offered = null;
        OfferedNotes = null;
        downloaded = false;
        var update = await manager.Value.CheckForUpdatesAsync();
        if (update is null) return null;
        var version = Version.Parse(update.TargetFullRelease.Version.ToString());
        if (version <= CurrentVersion || update.IsDowngrade || update.TargetFullRelease.PackageId != "AgentWindows")
            throw new InvalidDataException("Unsafe Windows update target.");
        if (source.HasManifest(update.TargetFullRelease))
            OfferedNotes = await EmbeddedWindowsNotes.TryLoadAsync(CurrentVersion, informationalVersion, update.TargetFullRelease, notesTransport);
        offered = update;
        return version;
    }

    private UpdateInfo RequireOffer(Version version)
    {
        if (offered is null || offered.TargetFullRelease.Version.ToString() != version.ToString() || version <= CurrentVersion)
            throw new InvalidOperationException("No matching newer Windows update.");
        return offered;
    }

    public async Task<ReleaseNotes?> RetryNotesAsync(Version version)
    {
        var target = RequireOffer(version).TargetFullRelease;
        if (source.HasManifest(target))
            OfferedNotes = await EmbeddedWindowsNotes.TryLoadAsync(CurrentVersion, informationalVersion, target, notesTransport);
        return OfferedNotes;
    }

    public async Task DownloadAsync(Version version, Action<int> progress, Action verifying, CancellationToken token)
    {
        downloaded = false;
        await manager.Value.DownloadAndVerifyAsync(RequireOffer(version), progress, verifying, token);
        token.ThrowIfCancellationRequested();
        downloaded = true;
    }

    public void ApplyAndRestart(Version version)
    {
        var update = RequireOffer(version);
        if (!downloaded) throw new InvalidOperationException("Download the Windows update first.");
        manager.Value.ApplyUpdatesAndRestart(update.TargetFullRelease, Array.Empty<string>());
    }

    private sealed class VerificationUpdateManager(IUpdateSource source, UpdateOptions options, IVelopackLocator? locator)
        : UpdateManager(source, options, locator)
    {
        private Action? onVerification;

        internal async Task DownloadAndVerifyAsync(UpdateInfo update, Action<int> progress, Action verifying, CancellationToken token)
        {
            bool verificationStarted = false;
            onVerification = () =>
            {
                token.ThrowIfCancellationRequested();
                verificationStarted = true;
                verifying();
            };
            try
            {
                await DownloadUpdatesAsync(update, progress, token).ConfigureAwait(false);
                if (verificationStarted) return;

                // Velopack 1.2.161 returns without hashing when the complete file is cached.
                // Reuse its verifier under the package lock, without rehashing fresh downloads.
                using var updateLock = await AcquireUpdateLock().ConfigureAwait(false);
                try { await VerifyPackageChecksumAsync(update.TargetFullRelease).ConfigureAwait(false); }
                catch (ChecksumFailedException ex)
                {
                    // Let the next explicit download replace a corrupt cache entry.
                    File.Delete(ex.FilePath);
                    throw;
                }
            }
            finally { onVerification = null; }
        }

        protected override Task VerifyPackageChecksumAsync(VelopackAsset release, string? filePathOverride = null)
        {
            onVerification?.Invoke();
            return base.VerifyPackageChecksumAsync(release, filePathOverride);
        }
    }
}
