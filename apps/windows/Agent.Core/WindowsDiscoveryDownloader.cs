using System.Buffers.Binary;
using System.Globalization;
using System.Net;
using System.Text;
using Velopack.Sources;

namespace Agent.Core;

/// <summary>Expose HTTP responses for inventory and feeds without changing Velopack package downloads.</summary>
internal sealed class WindowsDiscoveryDownloader(HttpClient http, string? cooldownPath, TimeProvider clock) : IFileDownloader
{
    private static readonly TimeSpan MaximumCooldown = TimeSpan.FromHours(1);
    internal static readonly HttpClient PublicHttp = new(new HttpClientHandler
    {
        AllowAutoRedirect = false, UseCookies = false, AutomaticDecompression = DecompressionMethods.All
    });
    private readonly IFileDownloader packages = new HttpClientFileDownloader();
    internal bool IsStartupCheckDeferred => cooldownPath is { } path && ReadActiveDeadline(path) is not null;

    public async Task<string> DownloadString(string url, IDictionary<string, string>? headers = null, double timeout = 30) =>
        Encoding.UTF8.GetString(await DownloadBytes(url, headers, timeout).ConfigureAwait(false));

    public async Task<byte[]> DownloadBytes(string url, IDictionary<string, string>? headers = null, double timeout = 30)
    {
        using var deadline = new CancellationTokenSource(TimeSpan.FromSeconds(timeout));
        var initial = new Uri(url);
        var uri = initial;
        for (int redirects = 0; ; redirects++)
        {
            using var request = new HttpRequestMessage(HttpMethod.Get, uri);
            if (headers is not null)
                foreach (var (name, value) in headers) request.Headers.TryAddWithoutValidation(name, value);
            if (request.Headers.UserAgent.Count == 0) request.Headers.UserAgent.ParseAdd("AgentWindows/0.1");
            using var response = await http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, deadline.Token).ConfigureAwait(false);
            if ((int)response.StatusCode is >= 300 and < 400)
            {
                if (initial.Host != "github.com" || redirects >= 3 || response.Headers.Location is not { } location ||
                    !Uri.TryCreate(uri, location, out var next) || !EmbeddedWindowsNotes.TrustedAsset(next, initial))
                    throw new InvalidDataException("Unexpected Windows release metadata redirect.");
                uri = next;
                continue;
            }
            RecordRateLimit(response);
            response.EnsureSuccessStatusCode();
            return await response.Content.ReadAsByteArrayAsync(deadline.Token).ConfigureAwait(false);
        }
    }

    private void RecordRateLimit(HttpResponseMessage response)
    {
        if (cooldownPath is not { } path || response.StatusCode is not (HttpStatusCode.Forbidden or HttpStatusCode.TooManyRequests)) return;
        var headers = response.Headers;
        var retryAfter = headers.RetryAfter;
        bool exhausted = headers.TryGetValues("X-RateLimit-Remaining", out var remaining) && remaining.FirstOrDefault() == "0";
        if (response.StatusCode == HttpStatusCode.Forbidden && retryAfter is null && !exhausted) return;

        var now = clock.GetUtcNow();
        var delay = retryAfter?.Delta ?? (retryAfter?.Date is { } date ? date - now : TimeSpan.Zero);
        if (exhausted && headers.TryGetValues("X-RateLimit-Reset", out var values) &&
            long.TryParse(values.FirstOrDefault(), NumberStyles.None, CultureInfo.InvariantCulture, out long reset) &&
            reset <= DateTimeOffset.MaxValue.ToUnixTimeSeconds())
        {
            var untilReset = DateTimeOffset.FromUnixTimeSeconds(reset) - now;
            if (untilReset > delay) delay = untilReset;
        }
        if (delay <= TimeSpan.Zero) delay = TimeSpan.FromMinutes(1);
        SaveDeadline(path, now.UtcTicks + Math.Min(delay.Ticks, MaximumCooldown.Ticks));
    }

    private long? ReadActiveDeadline(string path)
    {
        try
        {
            using var file = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.Read | FileShare.Delete);
            Span<byte> bytes = stackalloc byte[sizeof(long) + 1];
            if (file.ReadAtLeast(bytes, bytes.Length, throwOnEndOfStream: false) != sizeof(long)) return null;
            long deadline = BinaryPrimitives.ReadInt64LittleEndian(bytes);
            long now = clock.GetUtcNow().UtcTicks;
            return deadline > now && deadline <= now + MaximumCooldown.Ticks ? deadline : null;
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException) { return null; }
    }

    private void SaveDeadline(string path, long deadline)
    {
        // Persist only the deadline; a failed write is not a second discovery error.
        string temp = path + "." + Guid.NewGuid().ToString("N") + ".tmp";
        try
        {
            if (ReadActiveDeadline(path) >= deadline) return;
            Directory.CreateDirectory(Path.GetDirectoryName(path)!);
            Span<byte> bytes = stackalloc byte[sizeof(long)];
            BinaryPrimitives.WriteInt64LittleEndian(bytes, deadline);
            using (var file = new FileStream(temp, FileMode.CreateNew, FileAccess.Write)) file.Write(bytes);
            File.Move(temp, path, overwrite: true);
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            try { File.Delete(temp); }
            catch (Exception cleanup) when (cleanup is IOException or UnauthorizedAccessException) { }
        }
    }

    public Task DownloadFile(string url, string targetFile, Action<int> progress,
        IDictionary<string, string>? headers = null, double timeout = 30, CancellationToken cancelToken = default) =>
        packages.DownloadFile(url, targetFile, progress, headers, timeout, cancelToken);
}
