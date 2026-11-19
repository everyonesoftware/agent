using System.Net;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;

namespace Agent.Core;

public static class ServerEndpoint
{
    public static Uri Parse(string value)
    {
        if (!Uri.TryCreate(value.Trim(), UriKind.Absolute, out var uri) || !string.IsNullOrEmpty(uri.UserInfo) || !string.IsNullOrEmpty(uri.Query) || !string.IsNullOrEmpty(uri.Fragment) || uri.AbsolutePath != "/" || (uri.Scheme != "https" && !(uri.Scheme == "http" && uri.IsLoopback)))
            throw new ArgumentException("Use an HTTPS server origin (HTTP is allowed only for loopback development).");
        return new Uri(uri.GetLeftPart(UriPartial.Authority) + "/");
    }
}
public interface ISessionVault
{
    Task<string?> ReadAsync(string endpoint);
    Task SaveAsync(string endpoint, string token);
    Task RemoveAsync(string endpoint);
}
public class AgentException(string message, int status = 0) : Exception(message) { public int Status { get; } = status; }
public sealed class SessionExpiredException() : AgentException("Your Agent session expired. Sign in again.", 401);

/// <summary>Origin-bound cookie transport. Redirects and mutation retries are deliberately disabled.</summary>
public sealed class AgentClient : IDisposable
{
    private readonly HttpClient http;
    private readonly bool ownsHttp;
    private readonly ISessionVault vault;
    private readonly CancellationTokenSource lifetime = new();
    private string? token;
    public Uri Endpoint { get; }
    public event Action? SessionExpired;
    public AgentClient(Uri endpoint, ISessionVault vault, HttpClient? transport = null)
    {
        Endpoint = ServerEndpoint.Parse(endpoint.AbsoluteUri); this.vault = vault;
        ownsHttp = transport is null;
        http = transport ?? new HttpClient(new HttpClientHandler { AllowAutoRedirect = false, UseCookies = false }) { Timeout = TimeSpan.FromMinutes(3) };
    }
    public async Task<JsonNode?> RestoreAsync(CancellationToken cancellation = default)
    {
        token = await vault.ReadAsync(Endpoint.AbsoluteUri);
        return token is null ? null : await GetAsync("/api/auth/me", cancellation);
    }
    public async Task SignInAsync(string username, string password, bool register, CancellationToken cancellation = default)
    {
        token = null;
        await SendAsync(HttpMethod.Post, register ? "/api/auth/register" : "/api/auth/login", new { username, password }, cancellation, captureSession: true);
        if (token is null) throw new AgentException("The server did not establish a session.");
        try { await vault.SaveAsync(Endpoint.AbsoluteUri, token); }
        catch { token = null; throw new AgentException("Windows could not securely save your session. Sign in again."); }
    }
    public Task<JsonNode> GetAsync(string path, CancellationToken cancellation = default) => SendAsync(HttpMethod.Get, path, null, cancellation);
    public Task<JsonNode> PostAsync(string path, object? body = null, CancellationToken cancellation = default) => SendAsync(HttpMethod.Post, path, body ?? new { }, cancellation);
    public Task<JsonNode> PutAsync(string path, object body, CancellationToken cancellation = default) => SendAsync(HttpMethod.Put, path, body, cancellation);
    public Task<JsonNode> DeleteAsync(string path, string revision, CancellationToken cancellation = default) => SendAsync(HttpMethod.Delete, path, null, cancellation, revision: revision);
    public async Task SignOutAsync(CancellationToken cancellation = default)
    {
        await PostAsync("/api/auth/logout", cancellation: cancellation);
        await ForgetAsync();
    }
    public async Task ForgetAsync() { token = null; await vault.RemoveAsync(Endpoint.AbsoluteUri); }
    private async Task<JsonNode> SendAsync(HttpMethod method, string path, object? body, CancellationToken cancellation, bool captureSession = false, string? revision = null)
    {
        if (!(path.StartsWith("/api/", StringComparison.Ordinal) || path.StartsWith("/auth/start", StringComparison.Ordinal)) || path.Contains('\\') || path.Contains('#') || !Uri.TryCreate(Endpoint, path, out var target) || target.GetLeftPart(UriPartial.Authority) != Endpoint.GetLeftPart(UriPartial.Authority))
            throw new ArgumentException("Only Agent API paths on the selected server are allowed.");
        using var linked = CancellationTokenSource.CreateLinkedTokenSource(cancellation, lifetime.Token);
        using var request = new HttpRequestMessage(method, target);
        request.Headers.Accept.ParseAdd("application/json");
        if (!string.IsNullOrEmpty(token)) request.Headers.Add("Cookie", "everyoneagent_session=" + token);
        if (revision is not null) request.Headers.TryAddWithoutValidation("If-Match", JsonSerializer.Serialize(revision));
        if (body is not null) request.Content = new StringContent(JsonSerializer.Serialize(body), Encoding.UTF8, "application/json");
        using var response = await http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, linked.Token);
        if ((int)response.StatusCode is >= 300 and < 400) throw new AgentException("The server redirected this request. Check the server origin.", (int)response.StatusCode);
        if (response.StatusCode == HttpStatusCode.Unauthorized && !captureSession)
        {
            var error = await ReadErrorAsync(response, linked.Token);
            if (error.Action == "reauthorize") throw new AgentException("Reconnect ChatGPT in Settings. Your Agent session is still signed in.", 401);
            bool confirmation = path is "/api/auth/password" or "/api/auth/disconnect" or "/api/auth/profiles/active";
            bool sessionRejected = error.Error is "Sign in to continue." or "Sign in again before changing your password." or "Sign in again and confirm your current Agent password.";
            if (!confirmation || sessionRejected)
            {
                await ForgetAsync(); SessionExpired?.Invoke(); throw new SessionExpiredException();
            }
        }
        if (!response.IsSuccessStatusCode) throw new AgentException(response.StatusCode switch
        {
            HttpStatusCode.Unauthorized => "The password was not accepted.",
            HttpStatusCode.Conflict => "The server state changed. Refresh and review before retrying.",
            HttpStatusCode.TooManyRequests => "Too many requests. Wait before trying again.",
            HttpStatusCode.Forbidden => "This operation is not permitted. Review your connection and consent.",
            HttpStatusCode.BadRequest => "The server did not accept this input. Review the fields and try again.",
            _ => "The server request failed. Refresh to retry; changes are not automatically retried."
        }, (int)response.StatusCode);
        if ((captureSession || token is not null) && response.Headers.TryGetValues("Set-Cookie", out var cookies))
        {
            foreach (string cookie in cookies)
            {
                string pair = cookie.Split(';', 2)[0];
                if (!pair.StartsWith("everyoneagent_session=", StringComparison.Ordinal)) continue;
                string candidate = pair["everyoneagent_session=".Length..];
                bool deletion = cookie.Split(';').Skip(1).Any(part =>
                {
                    string[] attribute = part.Trim().Split('=', 2);
                    return attribute.Length == 2 && attribute[0].Equals("Max-Age", StringComparison.OrdinalIgnoreCase)
                        && long.TryParse(attribute[1], System.Globalization.NumberStyles.Integer, System.Globalization.CultureInfo.InvariantCulture, out long age) && age <= 0;
                });
                if (candidate.Length == 0 && deletion) { await ForgetAsync(); continue; }
                if (candidate.Length is < 1 or > 4096 || candidate.Any(c => !char.IsAsciiLetterOrDigit(c) && c is not '-' and not '_')) throw new AgentException("Invalid session from server.");
                token = candidate;
                if (!captureSession)
                {
                    try { await vault.SaveAsync(Endpoint.AbsoluteUri, candidate); }
                    catch { throw new AgentException("The server accepted the change, but Windows could not securely save the renewed session. Sign in again before closing the app."); }
                }
            }
        }
        await using var stream = await response.Content.ReadAsStreamAsync(linked.Token);
        using var bounded = new MemoryStream();
        var buffer = new byte[8192]; int read;
        while ((read = await stream.ReadAsync(buffer, linked.Token)) != 0) { if (bounded.Length + read > 8 * 1024 * 1024) throw new AgentException("The server response exceeded the client safety limit."); await bounded.WriteAsync(buffer.AsMemory(0, read), linked.Token); }
        linked.Token.ThrowIfCancellationRequested();
        return bounded.Length == 0 ? new JsonObject() : JsonNode.Parse(bounded.ToArray()) ?? new JsonObject();
    }
    private static async Task<(string? Action, string? Error)> ReadErrorAsync(HttpResponseMessage response, CancellationToken token)
    {
        await using var stream = await response.Content.ReadAsStreamAsync(token);
        using var bytes = new MemoryStream();
        byte[] buffer = new byte[4096]; int read;
        while ((read = await stream.ReadAsync(buffer, token)) != 0)
        {
            if (bytes.Length + read > 65536) throw new AgentException("The server error response exceeded the client safety limit.", 401);
            await bytes.WriteAsync(buffer.AsMemory(0, read), token);
        }
        try
        {
            using var json = JsonDocument.Parse(bytes.ToArray());
            if (json.RootElement.ValueKind != JsonValueKind.Object) return (null, null);
            string? action = json.RootElement.TryGetProperty("action", out var a) && a.ValueKind == JsonValueKind.String ? a.GetString() : null;
            string? error = json.RootElement.TryGetProperty("error", out var e) && e.ValueKind == JsonValueKind.String ? e.GetString() : null;
            return (action, error);
        }
        catch (JsonException) { return (null, null); }
    }
    public void Dispose() { lifetime.Cancel(); lifetime.Dispose(); if (ownsHttp) http.Dispose(); }
}
public static class Wire
{
    public static string Text(this JsonNode? node, string key, string fallback = "") => node?[key]?.GetValue<string>() ?? fallback;
    public static bool Flag(this JsonNode? node, string key) => node?[key]?.GetValue<bool>() ?? false;
    public static IEnumerable<JsonNode> Items(this JsonNode? node, string key) => (node?[key] as JsonArray)?.OfType<JsonNode>() ?? [];
    public static string Segment(string value) => Uri.EscapeDataString(value);
}
