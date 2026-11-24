using System.Net;
using System.Text;
using System.Text.Json.Nodes;
using Agent.Core;
using Velopack.Locators;
using Velopack.Logging;
using Velopack.Sources;
using Xunit;

namespace Agent.Core.Tests;

public sealed class EmbeddedNotesTests
{
    private const string Hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
    private const string Package = "AgentWindows-0.1.5-win-x64-full.nupkg";
    private static string Sha(int n) => n.ToString("x40");

    [Theory]
    [InlineData("inventory", 429, "delta")]
    [InlineData("inventory", 429, "short-delta")]
    [InlineData("feed", 429, "date")]
    [InlineData("inventory", 403, "reset")]
    [InlineData("inventory", 429, "none")]
    public async Task RateLimitedDiscoveryDefersFreshLaunchButNotManualCheck(string stage, int statusCode, string signal)
    {
        using var fixture = new Fixture { RateLimitedStage = stage, RateLimitStatus = (HttpStatusCode)statusCode, RateLimitSignal = signal };
        using (var first = fixture.Controller(useDiscoveryHttp: true))
        {
            await first.CheckOnStartupAsync();
            Assert.Equal(WindowsUpdatePhase.Error, first.Status.Phase);
        }
        fixture.Requests.Clear();
        fixture.RateLimitedStage = null;
        using (var relaunch = fixture.Controller(useDiscoveryHttp: true))
        {
            await relaunch.CheckOnStartupAsync();
            Assert.Equal(WindowsUpdatePhase.Idle, relaunch.Status.Phase);
            Assert.Empty(fixture.Requests);
            await relaunch.CheckAsync();
            Assert.Equal(WindowsUpdatePhase.Available, relaunch.Status.Phase);
            Assert.Equal(new[] { "inventory", "feed", "manifest" }, fixture.Requests);
        }
        fixture.Requests.Clear();
        // Honor short deadlines, cap large ones, and fall back for an unqualified 429.
        fixture.Clock.Now += signal switch
        {
            "none" => TimeSpan.FromMinutes(1),
            "short-delta" => TimeSpan.FromSeconds(20),
            _ => TimeSpan.FromHours(1)
        };
        using var expired = fixture.Controller(useDiscoveryHttp: true);
        await expired.CheckOnStartupAsync();
        Assert.Equal(WindowsUpdatePhase.Available, expired.Status.Phase);
        Assert.Equal(new[] { "inventory", "feed", "manifest" }, fixture.Requests);
    }

    [Theory]
    [InlineData(HttpStatusCode.Forbidden)]
    [InlineData(HttpStatusCode.ServiceUnavailable)]
    public async Task OrdinaryDiscoveryFailuresDoNotSetCooldown(HttpStatusCode status)
    {
        using var fixture = new Fixture { RateLimitedStage = "inventory", RateLimitStatus = status };
        using (var first = fixture.Controller(useDiscoveryHttp: true))
        {
            await first.CheckOnStartupAsync();
            Assert.Equal(WindowsUpdatePhase.Error, first.Status.Phase);
        }
        fixture.Requests.Clear();
        fixture.RateLimitedStage = null;
        using var relaunch = fixture.Controller(useDiscoveryHttp: true);
        await relaunch.CheckOnStartupAsync();
        Assert.Equal(WindowsUpdatePhase.Available, relaunch.Status.Phase);
        Assert.Equal(new[] { "inventory", "feed", "manifest" }, fixture.Requests);
    }

    [Fact]
    public async Task MalformedCooldownDoesNotSuppressDiscovery()
    {
        using var fixture = new Fixture();
        await File.WriteAllTextAsync(fixture.CooldownPath, "invalid marker");
        using var controller = fixture.Controller(useDiscoveryHttp: true);
        await controller.CheckOnStartupAsync();
        Assert.Equal(WindowsUpdatePhase.Available, controller.Status.Phase);
        Assert.Equal(new[] { "inventory", "feed", "manifest" }, fixture.Requests);
    }

    [Fact]
    public async Task DiscoveryCachesExactRangeUsingRealVelopackOffer()
    {
        using var fixture = new Fixture();
        using var controller = fixture.Controller();
        await controller.CheckOnStartupAsync();
        Assert.Equal(WindowsUpdatePhase.Available, controller.Status.Phase);
        var notes = Assert.IsType<ReleaseNotes>(controller.Status.Notes);
        Assert.Equal(Sha(2), notes.From);
        Assert.Equal(Sha(5), notes.To);
        // The sibling branch diverged before the installed commit but is new to the offered package.
        Assert.Equal(new[] { Sha(3), Sha(4), Sha(5) }, notes.Commits.Select(x => x.Sha));
        Assert.Equal("fix(windows): Original subject", notes.Commits[0].Subject);
        Assert.Equal(new[] { "Published improvement" }, notes.Improvements);
        Assert.Equal(new[] { "Published fix" }, notes.Fixes);
        Assert.Same(notes, controller.Status.Notes);
        Assert.Equal(new[] { "inventory", "feed", "manifest" }, fixture.Requests);
        fixture.Body.Remove("changelog");
        await controller.CheckAsync();
        Assert.Equal(WindowsUpdatePhase.Available, controller.Status.Phase);
        Assert.Null(controller.Status.Notes);
        Assert.Equal(new[] { "inventory", "feed", "manifest", "inventory", "feed", "manifest" }, fixture.Requests);
    }

    [Fact]
    public async Task ChangelogRetryKeepsExactOfferAcrossFailureAndRecoveryWithoutRediscovery()
    {
        using var fixture = new Fixture { ResponseStatus = HttpStatusCode.ServiceUnavailable };
        using var controller = fixture.Controller();
        await controller.CheckAsync();
        var offered = controller.Status;
        Assert.Equal(WindowsUpdatePhase.Available, offered.Phase);
        Assert.Null(offered.Notes);
        fixture.InventoryUnavailable = true;
        var states = new List<WindowsUpdateStatus>();
        controller.Changed += states.Add;

        await controller.RetryChangelogAsync();
        Assert.Equal(offered, controller.Status);
        fixture.ResponseStatus = HttpStatusCode.OK;
        await controller.RetryChangelogAsync();

        Assert.Equal(Sha(5), Assert.IsType<ReleaseNotes>(controller.Status.Notes).To);
        Assert.All(states, s => { Assert.Equal(offered.Version, s.Version); Assert.Equal(WindowsUpdatePhase.Available, s.Phase); });
        Assert.Equal(new[] { "inventory", "feed", "manifest", "manifest", "manifest" }, fixture.Requests);
    }

    [Theory]
    [InlineData("legacy")]
    [InlineData("truncated")]
    [InlineData("invalid-sha")]
    [InlineData("wrong-digest")]
    [InlineData("wrong-size")]
    [InlineData("wrong-package")]
    [InlineData("wrong-version")]
    [InlineData("wrong-commit-count")]
    [InlineData("missing-intermediate")]
    [InlineData("invalid-group")]
    [InlineData("oversize")]
    [InlineData("duplicate-property")]
    [InlineData("not-found")]
    public async Task InvalidOptionalManifestDoesNotBlockOffer(string fault)
    {
        using var fixture = new Fixture();
        var history = fixture.Body["changelog"]!.AsObject();
        switch (fault)
        {
            case "legacy": fixture.Body.Remove("changelog"); break;
            case "truncated": history["truncated"] = true; break;
            case "invalid-sha": history["commits"]![2]!["sha"] = "bad"; break;
            case "wrong-digest": fixture.Body["package"]!["sha256"] = new string('b', 64); break;
            case "wrong-size": fixture.Body["package"]!["size"] = 11; break;
            case "wrong-package": fixture.Body["package"]!["name"] = "other.nupkg"; break;
            case "wrong-version": fixture.Body["version"] = "0.1.6"; break;
            case "wrong-commit-count": fixture.Body["versionCode"] = 4; break;
            case "missing-intermediate": history["commits"]!.AsArray().RemoveAt(2); break;
            case "invalid-group": history["fixes"]![0]!["commit"] = Sha(9); break;
            case "oversize": fixture.Body["pad"] = new string('x', 1024 * 1024); break;
            case "duplicate-property": fixture.OverrideBody = "{\"version\":\"0.1.4\"," + fixture.Body.ToJsonString()[1..]; break;
            case "not-found": fixture.ResponseStatus = HttpStatusCode.NotFound; break;
        }
        using var controller = fixture.Controller();
        await controller.CheckAsync();
        Assert.Equal(WindowsUpdatePhase.Available, controller.Status.Phase);
        Assert.Equal(new Version(0, 1, 5), controller.Status.Version);
        Assert.Null(controller.Status.Notes);
        Assert.Equal(new[] { "inventory", "feed", "manifest" }, fixture.Requests);
    }

    [Theory]
    [InlineData("0.1.5")]
    [InlineData("0.1.2+bad")]
    [InlineData("0.1.1+")]
    public async Task NoManifestRequestForCurrentBuildOrUnverifiedInstalledIdentity(string identity)
    {
        using var fixture = new Fixture();
        var current = identity == "0.1.5";
        using var controller = fixture.Controller(identity, current ? "0.1.5" : "0.1.2");
        await controller.CheckAsync();
        Assert.Equal(current ? WindowsUpdatePhase.Current : WindowsUpdatePhase.Available, controller.Status.Phase);
        Assert.Null(controller.Status.Notes);
        Assert.Equal(new[] { "inventory", "feed" }, fixture.Requests);
    }

    [Fact]
    public async Task AssetRedirectsAreBoundedAndNeverContactForeignHosts()
    {
        using var fixture = new Fixture();
        fixture.Redirect = "https://example.test/not-an-agent-asset";
        using var controller = fixture.Controller();
        await controller.CheckAsync();
        Assert.Equal(WindowsUpdatePhase.Available, controller.Status.Phase);
        Assert.Null(controller.Status.Notes);
        Assert.Equal(new[] { "inventory", "feed", "manifest" }, fixture.Requests);
        fixture.Redirect = "https://release-assets.githubusercontent.com/github-production-release-asset/test";
        await controller.CheckAsync();
        Assert.NotNull(controller.Status.Notes);
        Assert.Equal(new[] { "inventory", "feed", "manifest", "inventory", "feed", "manifest", "cdn" }, fixture.Requests);
    }

    [Theory]
    [InlineData("missing")]
    [InlineData("malformed")]
    [InlineData("wrong-origin")]
    [InlineData("duplicate")]
    public async Task UnusableManifestAssetDoesNotBlockThePackage(string metadata)
    {
        using var fixture = new Fixture { ManifestAsset = metadata };
        using var controller = fixture.Controller();
        await controller.CheckAsync();
        Assert.Equal(WindowsUpdatePhase.Available, controller.Status.Phase);
        Assert.Null(controller.Status.Notes);
        Assert.Equal(new[] { "inventory", "feed" }, fixture.Requests);
    }

    [Theory]
    [InlineData(false, false)]
    [InlineData(false, true)]
    [InlineData(true, false)]
    [InlineData(true, true)]
    public async Task RealVelopackChecksPackageBeforeReady(bool cached, bool badChecksum)
    {
        byte[] expected = Encoding.UTF8.GetBytes("0123456789");
        byte[] actual = badChecksum ? Encoding.UTF8.GetBytes("0123456780") : expected;
        using var fixture = new Fixture
        {
            ManifestAsset = "missing",
            PackageBytes = actual,
            PackageHash = Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(expected))
        };
        bool preserveSession = false;
        using var controller = fixture.Controller(preserveSession: () => { preserveSession = true; return Task.CompletedTask; });
        await controller.CheckAsync();
        Assert.Equal(WindowsUpdatePhase.Available, controller.Status.Phase);
        if (cached) await File.WriteAllBytesAsync(fixture.PackagePath, actual);

        var verifying = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        using var resume = new ManualResetEventSlim();
        var statuses = new List<WindowsUpdateStatus>();
        controller.Changed += status =>
        {
            statuses.Add(status);
            if (status.Phase == WindowsUpdatePhase.Verifying)
            {
                verifying.TrySetResult();
                resume.Wait(TimeSpan.FromSeconds(10));
            }
        };
        var download = Task.Run(controller.DownloadAsync);
        try
        {
            Assert.Same(verifying.Task, await Task.WhenAny(verifying.Task, download).WaitAsync(TimeSpan.FromSeconds(5)));
            Assert.Equal(WindowsUpdatePhase.Verifying, controller.Status.Phase);
            Assert.False(download.IsCompleted);
            // Fresh downloads must still be in Velopack's partial file, not already verified and moved.
            Assert.Equal(cached, File.Exists(fixture.PackagePath));
            Assert.Equal(!cached, File.Exists(fixture.PackagePath + ".partial"));
            await controller.RestartAsync();
            Assert.False(preserveSession);
        }
        finally
        {
            resume.Set();
            await download.WaitAsync(TimeSpan.FromSeconds(5));
        }

        Assert.Equal(badChecksum ? WindowsUpdatePhase.Error : WindowsUpdatePhase.Ready, controller.Status.Phase);
        Assert.Single(statuses, status => status.Phase == WindowsUpdatePhase.Verifying);
        Assert.Equal(cached ? 0 : 1, fixture.Requests.Count(request => request == "package"));
        if (badChecksum)
        {
            Assert.DoesNotContain(statuses, status => status.Phase == WindowsUpdatePhase.Ready);
            Assert.False(File.Exists(fixture.PackagePath));
            await controller.RestartAsync();
            Assert.False(preserveSession);
        }
    }

    private sealed class Fixture : HttpMessageHandler, IFileDownloader
    {
        private readonly DirectoryInfo directory = Directory.CreateTempSubdirectory("agent-windows-notes-");
        public string CooldownPath => Path.Combine(directory.FullName, "cooldown");
        public string PackagePath => Path.Combine(directory.FullName, Package);
        public byte[]? PackageBytes;
        public string PackageHash = Hash;
        public List<string> Requests { get; } = [];
        public string? Redirect, OverrideBody, ManifestAsset;
        public bool InventoryUnavailable;
        public string? RateLimitedStage, RateLimitSignal;
        public HttpStatusCode RateLimitStatus;
        public TestClock Clock { get; } = new();
        public sealed class TestClock : TimeProvider
        {
            public DateTimeOffset Now = new(2026, 1, 1, 0, 0, 0, TimeSpan.Zero);
            public override DateTimeOffset GetUtcNow() => Now;
        }
        public HttpStatusCode ResponseStatus = HttpStatusCode.OK;
        public JsonObject Body { get; } = new()
        {
            ["schemaVersion"] = 1, ["platform"] = "windows", ["version"] = "0.1.5", ["versionCode"] = 5, ["commit"] = Sha(5),
            ["package"] = new JsonObject { ["name"] = Package, ["size"] = 10, ["sha256"] = Hash },
            ["changelog"] = new JsonObject
            {
                ["schemaVersion"] = 1, ["truncated"] = false,
                ["commits"] = new JsonArray(Commit(1, "Root"), Commit(2, "Already installed", 1), Commit(3, "fix(windows): Original subject", 1), Commit(4, "Merge", 2, 3), Commit(5, "Windows release", 4)),
                ["improvements"] = new JsonArray(Bullet(2, "Already installed"), Bullet(5, "Published improvement")),
                ["fixes"] = new JsonArray(Bullet(3, "Published fix"))
            }
        };
        private static JsonObject Commit(int id, string subject, params int[] parents) => new()
        {
            ["sha"] = Sha(id), ["parents"] = new JsonArray(parents.Select(p => (JsonNode?)JsonValue.Create(Sha(p))).ToArray()), ["subject"] = subject
        };
        private static JsonObject Bullet(int id, string text) => new() { ["commit"] = Sha(id), ["text"] = text };
        public WindowsUpdateController Controller(string? informationalVersion = null, string installed = "0.1.2", bool useDiscoveryHttp = false, Func<Task>? preserveSession = null)
        {
            var locator = new TestVelopackLocator("AgentWindows", installed, directory.FullName, logger: NullVelopackLogger.Instance);
            var http = new HttpClient(this, disposeHandler: false);
            var source = useDiscoveryHttp ? new WindowsGithubSource(http, CooldownPath, Clock) : new WindowsGithubSource(this);
            var backend = new WindowsUpdateBackend(informationalVersion ?? $"0.1.2+{Sha(2)}", source, locator, http);
            return new(backend, preserveSession ?? (() => Task.CompletedTask));
        }
        protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            Assert.Null(request.Headers.Authorization);
            Assert.False(request.Headers.Contains("Cookie"));
            string url = request.RequestUri!.AbsoluteUri;
            if (request.RequestUri.Host == "api.github.com" || url.EndsWith("/releases.win-x64.json"))
            {
                string stage = request.RequestUri.Host == "api.github.com" ? "inventory" : "feed";
                if (RateLimitedStage == stage)
                {
                    Requests.Add(stage);
                    var response = new HttpResponseMessage(RateLimitStatus);
                    if (RateLimitSignal == "delta") response.Headers.TryAddWithoutValidation("Retry-After", "7200");
                    if (RateLimitSignal == "short-delta") response.Headers.TryAddWithoutValidation("Retry-After", "20");
                    if (RateLimitSignal == "date") response.Headers.RetryAfter = new(Clock.Now.AddHours(2));
                    if (RateLimitSignal == "reset")
                    {
                        response.Headers.TryAddWithoutValidation("X-RateLimit-Remaining", "0");
                        response.Headers.TryAddWithoutValidation("X-RateLimit-Reset", Clock.Now.AddHours(2).ToUnixTimeSeconds().ToString());
                    }
                    return response;
                }
                return new HttpResponseMessage(HttpStatusCode.OK) { Content = stage == "inventory"
                    ? new StringContent(await DownloadString(url)) : new ByteArrayContent(await DownloadBytes(url)) };
            }
            if (request.RequestUri!.Host == "github.com")
            {
                Assert.Equal("/everyonesoftware/agent/releases/download/v0.1.5/agent-windows-release.json", request.RequestUri.AbsolutePath);
                Requests.Add("manifest");
                if (Redirect is not null) return new HttpResponseMessage(HttpStatusCode.Found) { Headers = { Location = new Uri(Redirect) } };
            }
            else
            {
                Assert.Equal("release-assets.githubusercontent.com", request.RequestUri.Host);
                Requests.Add("cdn");
            }
            return new HttpResponseMessage(ResponseStatus) { Content = new StringContent(OverrideBody ?? Body.ToJsonString()) };
        }
        public Task<string> DownloadString(string url, IDictionary<string,string>? headers = null, double timeout = 30)
        {
            Assert.EndsWith("/releases?per_page=100&page=1", url);
            Requests.Add("inventory");
            if (InventoryUnavailable) throw new HttpRequestException("Release inventory unavailable.");
            const string manifest = "agent-windows-release.json";
            object Asset(string name) => new { name, browser_download_url = $"{WindowsGithubSource.Repository}/releases/download/v0.1.5/{name}" };
            var assets = new List<object> { Asset("releases.win-x64.json"), Asset(Package) };
            if (ManifestAsset != "missing") assets.Add(ManifestAsset switch
            {
                "malformed" => new { name = manifest, browser_download_url = 42 },
                "wrong-origin" => new { name = manifest, browser_download_url = "https://example.test/manifest" },
                _ => Asset(manifest)
            });
            if (ManifestAsset == "duplicate") assets.Add(Asset(manifest));
            return Task.FromResult(System.Text.Json.JsonSerializer.Serialize(new[] { new { tag_name = "v0.1.5", draft = false, prerelease = false, assets } }));
        }
        public Task<byte[]> DownloadBytes(string url, IDictionary<string,string>? headers = null, double timeout = 30)
        {
            Assert.EndsWith("/v0.1.5/releases.win-x64.json", url);
            Requests.Add("feed");
            return Task.FromResult(Encoding.UTF8.GetBytes($$"""{"Assets":[{"PackageId":"AgentWindows","Version":"0.1.5","Type":"Full","FileName":"{{Package}}","Size":10,"SHA256":"{{PackageHash}}"}]}"""));
        }
        public async Task DownloadFile(string url, string targetFile, Action<int>? progress = null, IDictionary<string,string>? headers = null, double timeout = 30, CancellationToken cancelToken = default)
        {
            if (PackageBytes is null) throw new InvalidOperationException("Discovery must not download the package.");
            Assert.EndsWith($"/v0.1.5/{Package}", url);
            Requests.Add("package");
            await File.WriteAllBytesAsync(targetFile, PackageBytes, cancelToken);
            progress?.Invoke(100);
        }
        protected override void Dispose(bool disposing) { if (disposing) directory.Delete(true); base.Dispose(disposing); }
    }
}
