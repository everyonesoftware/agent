using System.Text.Json;
using System.Text.RegularExpressions;
using Velopack;

namespace Agent.Core;

public sealed record SourceCommit(string Sha, string Subject);
public sealed record ReleaseNotes(string From, string To, IReadOnlyList<SourceCommit> Commits,
    IReadOnlyList<string> Improvements, IReadOnlyList<string> Fixes);

internal static partial class EmbeddedWindowsNotes
{
    internal const string ManifestName = "agent-windows-release.json";
    private const int MaxBytes = 1024 * 1024;
    private const int MaxCommits = 2048;
    private const int MaxRedirects = 3;
    private static readonly HttpClient AnonymousTransport = new(new HttpClientHandler { AllowAutoRedirect = false, UseCookies = false });

    [GeneratedRegex(@"\A[0-9a-f]{40}\z", RegexOptions.CultureInvariant)]
    private static partial Regex SourceSha();
    [GeneratedRegex(@"\A[0-9a-f]{64}\z", RegexOptions.CultureInvariant)]
    private static partial Regex Sha256();

    internal static async Task<ReleaseNotes?> TryLoadAsync(Version installed, string? informationalVersion,
        VelopackAsset offer, HttpClient? transport)
    {
        if (informationalVersion?.Split('+') is not [var version, var from] || version != installed.ToString() ||
            !SourceSha().IsMatch(from) || installed.Build is < 1 or > MaxCommits || offer.Size <= 0 ||
            offer.SHA256 is not { } digest || !Sha256().IsMatch(digest.ToLowerInvariant())) return null;
        var offeredVersion = Version.Parse(offer.Version.ToString());
        if (offeredVersion.Build is < 1 or > MaxCommits) return null;

        try
        {
            using var deadline = new CancellationTokenSource(TimeSpan.FromSeconds(10));
            var uri = new Uri($"{WindowsGithubSource.Repository}/releases/download/v{offeredVersion}/{ManifestName}");
            using var document = await ReadAsync(transport ?? AnonymousTransport, uri, deadline.Token);
            return Parse(document.RootElement, offer, offeredVersion, installed, from);
        }
        catch (Exception ex) when (ex is HttpRequestException or IOException or OperationCanceledException or JsonException
            or InvalidDataException or InvalidOperationException or KeyNotFoundException or FormatException)
        {
            return null; // Optional metadata must never invalidate the independently verified offer.
        }
    }

    private static async Task<JsonDocument> ReadAsync(HttpClient transport, Uri initial, CancellationToken token)
    {
        var uri = initial;
        for (int redirects = 0; ; redirects++)
        {
            using var request = new HttpRequestMessage(HttpMethod.Get, uri);
            request.Headers.UserAgent.ParseAdd("AgentWindows/0.1");
            using var response = await transport.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, token);
            if ((int)response.StatusCode is >= 300 and < 400)
            {
                if (redirects >= MaxRedirects || response.Headers.Location is not { } location ||
                    !Uri.TryCreate(uri, location, out var next) || !TrustedAsset(next, initial))
                    throw new InvalidDataException("Unexpected Windows manifest redirect.");
                uri = next;
                continue;
            }
            response.EnsureSuccessStatusCode();
            if (response.Content.Headers.ContentLength > MaxBytes) throw new InvalidDataException("Windows manifest too large.");
            using var input = await response.Content.ReadAsStreamAsync(token);
            using var bytes = new MemoryStream();
            var buffer = new byte[8192];
            while (true)
            {
                int read = await input.ReadAsync(buffer.AsMemory(0, (int)Math.Min(buffer.Length, MaxBytes - bytes.Length + 1)), token);
                if (read == 0) return JsonDocument.Parse(bytes.GetBuffer().AsMemory(0, (int)bytes.Length));
                if (bytes.Length + read > MaxBytes) throw new InvalidDataException("Windows manifest too large.");
                bytes.Write(buffer, 0, read);
            }
        }
    }

    internal static bool TrustedAsset(Uri uri, Uri initial) => uri.Scheme == Uri.UriSchemeHttps && uri.IsDefaultPort &&
        uri.UserInfo.Length == 0 && uri.Fragment.Length == 0 &&
        (uri == initial || (uri.Host is "release-assets.githubusercontent.com" or "objects.githubusercontent.com" or "github-releases.githubusercontent.com" &&
            uri.AbsolutePath.StartsWith("/github-production-release-asset/", StringComparison.Ordinal)));

    private static ReleaseNotes Parse(JsonElement manifest, VelopackAsset offer, Version offeredVersion, Version installed, string from)
    {
        RequireUniqueProperties(manifest);
        var package = manifest.GetProperty("package");
        var to = ReadSha(manifest.GetProperty("commit"));
        if (manifest.GetProperty("schemaVersion").GetInt32() != 1 || manifest.GetProperty("platform").GetString() != "windows" ||
            manifest.GetProperty("version").GetString() != offeredVersion.ToString() || manifest.GetProperty("versionCode").GetInt32() != offeredVersion.Build ||
            package.GetProperty("name").GetString() != offer.FileName || package.GetProperty("size").GetInt64() != offer.Size ||
            package.GetProperty("sha256").GetString() is not { } digest || !Sha256().IsMatch(digest) ||
            !digest.Equals(offer.SHA256, StringComparison.OrdinalIgnoreCase)) throw InvalidHistory();

        var history = manifest.GetProperty("changelog");
        var entries = history.GetProperty("commits");
        if (history.GetProperty("schemaVersion").GetInt32() != 1 || history.GetProperty("truncated").GetBoolean() ||
            entries.GetArrayLength() != offeredVersion.Build) throw InvalidHistory();
        var commits = new List<SourceCommit>(entries.GetArrayLength());
        var graph = new Dictionary<string, string[]>(StringComparer.Ordinal);
        foreach (var entry in entries.EnumerateArray())
        {
            string sha = ReadSha(entry.GetProperty("sha"));
            var parents = entry.GetProperty("parents").EnumerateArray().Select(ReadSha).ToArray();
            if (parents.Distinct(StringComparer.Ordinal).Count() != parents.Length || parents.Any(p => !graph.ContainsKey(p)) ||
                !graph.TryAdd(sha, parents)) throw InvalidHistory();
            // Keep the original first line (including a possible CR); do not derive user-facing summaries here.
            if (entry.GetProperty("subject").GetString() is not { } subject || subject.Contains('\n')) throw InvalidHistory();
            commits.Add(new(sha, subject));
        }

        if (commits[^1].Sha != to || !graph.ContainsKey(from)) throw InvalidHistory();
        var offered = Ancestors(to, graph);
        var baseline = Ancestors(from, graph);
        if (!offered.Contains(from) || offered.Count != offeredVersion.Build || baseline.Count != installed.Build) throw InvalidHistory();
        offered.ExceptWith(baseline);
        if (offered.Count != offeredVersion.Build - installed.Build) throw InvalidHistory();

        var grouped = new HashSet<string>(StringComparer.Ordinal);
        var improvements = Group(history.GetProperty("improvements"));
        var fixes = Group(history.GetProperty("fixes"));
        return new(from, to, commits.Where(commit => offered.Contains(commit.Sha)).ToList().AsReadOnly(), improvements, fixes);

        IReadOnlyList<string> Group(JsonElement group)
        {
            var bullets = new List<string>();
            foreach (var item in group.EnumerateArray())
            {
                string sha = ReadSha(item.GetProperty("commit"));
                var text = item.GetProperty("text").GetString();
                if (!graph.ContainsKey(sha) || !grouped.Add(sha) || string.IsNullOrWhiteSpace(text) ||
                    text.AsSpan().ContainsAny('\r', '\n')) throw InvalidHistory();
                if (offered.Contains(sha)) bullets.Add(text);
            }
            return bullets.AsReadOnly();
        }
    }

    private static HashSet<string> Ancestors(string root, Dictionary<string, string[]> graph)
    {
        var seen = new HashSet<string>(StringComparer.Ordinal) { root };
        var pending = new Stack<string>();
        pending.Push(root);
        while (pending.TryPop(out var current))
            foreach (var parent in graph[current])
                if (seen.Add(parent)) pending.Push(parent);
        return seen;
    }

    private static void RequireUniqueProperties(JsonElement element)
    {
        if (element.ValueKind == JsonValueKind.Object)
        {
            var names = new HashSet<string>(StringComparer.Ordinal);
            foreach (var property in element.EnumerateObject())
            {
                if (!names.Add(property.Name)) throw InvalidHistory();
                RequireUniqueProperties(property.Value);
            }
        }
        else if (element.ValueKind == JsonValueKind.Array)
            foreach (var value in element.EnumerateArray()) RequireUniqueProperties(value);
    }

    private static string ReadSha(JsonElement value) => value.GetString() is { } sha && SourceSha().IsMatch(sha) ? sha : throw InvalidHistory();
    private static InvalidDataException InvalidHistory() => new("Invalid or incomplete Windows changelog.");
}
