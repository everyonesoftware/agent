using System.Text;
using System.Text.Json;
using Agent.Core;
using Velopack;
using Velopack.Logging;
using Velopack.Sources;
using Xunit;
namespace Agent.Core.Tests;
public sealed class UpdateTests
{
    [Fact] public async Task DiscoversWindowsBeyondMixedFirstPageWithoutLatest()
    {
        var download=new Download();var feed=await new WindowsGithubSource(download).GetReleaseFeed(NullVelopackLogger.Instance,"AgentWindows","win-x64");
        Assert.Equal("0.1.20",Assert.Single(feed.Assets).Version.ToString());Assert.Contains(download.Urls,u=>u.EndsWith("page=2"));Assert.DoesNotContain(download.Urls,u=>u.Contains("latest"));
    }
    [Theory][InlineData("wrong-version")][InlineData("missing-package")][InlineData("empty")][InlineData("duplicate")][InlineData("wrong-origin")]
    public async Task HighestInvalidFeedFailsClosed(string fault)=>await Assert.ThrowsAsync<InvalidDataException>(()=>new WindowsGithubSource(new Download{Fault=fault}).GetReleaseFeed(NullVelopackLogger.Instance,"AgentWindows","win-x64"));
    [Fact] public async Task FullTenthPageNeverClaimsCurrent()=>await Assert.ThrowsAsync<InvalidDataException>(()=>new WindowsGithubSource(new Download{Truncated=true}).GetReleaseFeed(NullVelopackLogger.Instance,"AgentWindows","win-x64"));
    [Fact] public async Task NoAutomaticWorkAndExplicitDownloadThenRestart()
    {
        var backend=new Backend();var saved=false;using var controller=new WindowsUpdateController(backend,()=>{saved=true;return Task.CompletedTask;});
        Assert.Equal(0,backend.Checks);Assert.Equal(WindowsUpdatePhase.Idle,controller.Status.Phase);
        await controller.RestartAsync();Assert.False(backend.Applied);await controller.CheckAsync();Assert.Equal(WindowsUpdatePhase.Available,controller.Status.Phase);
        await controller.RestartAsync();Assert.False(backend.Applied);await controller.DownloadAsync();Assert.Equal(WindowsUpdatePhase.Ready,controller.Status.Phase);Assert.False(backend.Applied);
        Assert.Empty(controller.Status.Message);
        await controller.RestartAsync();Assert.True(saved);Assert.True(backend.Applied);
    }
    [Fact] public async Task FailedSessionPersistenceBlocksApply()
    {
        var backend=new Backend();using var controller=new WindowsUpdateController(backend,()=>Task.FromException(new IOException()));await controller.CheckAsync();await controller.DownloadAsync();await controller.RestartAsync();Assert.False(backend.Applied);Assert.Equal(WindowsUpdatePhase.Ready,controller.Status.Phase);
        Assert.NotEmpty(controller.Status.Message);
    }
    [Fact]
    public async Task CachedNotesSurviveDownloadAndFailedRestart()
    {
        var notes = new ReleaseNotes("from", "to", [], [], []);
        var backend = new Backend { OfferedNotes = notes };
        using var controller = new WindowsUpdateController(backend, () => Task.FromException(new IOException()));
        var updates = new List<WindowsUpdateStatus>();
        controller.Changed += updates.Add;
        await controller.CheckAsync();
        await controller.DownloadAsync();
        await controller.RestartAsync();
        Assert.Equal(WindowsUpdatePhase.Ready, controller.Status.Phase);
        Assert.NotEmpty(controller.Status.Message);
        Assert.Equal(1, backend.Checks);
        Assert.Contains(updates, status => status is { Phase: WindowsUpdatePhase.Downloading, Progress: 100 });
        Assert.All(updates.Where(status => status.Version is not null), status => Assert.Same(notes, status.Notes));
    }
    [Fact]
    public async Task StartupReservesDiscoveryWhileLocatorIsBlockedAndRunsOnlyOnce()
    {
        using var unblock = new ManualResetEventSlim();
        var entered = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var backend = new Backend { Locate = () => { entered.TrySetResult(); return unblock.Wait(TimeSpan.FromSeconds(10)); } };
        using var controller = new WindowsUpdateController(backend, () => Task.CompletedTask);
        var startup = controller.CheckOnStartupAsync();
        try
        {
            await entered.Task.WaitAsync(TimeSpan.FromSeconds(5));
            await controller.CheckAsync().WaitAsync(TimeSpan.FromSeconds(5));
            await controller.CheckOnStartupAsync().WaitAsync(TimeSpan.FromSeconds(5));
            Assert.Equal(0, backend.Checks);
        }
        finally { unblock.Set(); }
        await startup.WaitAsync(TimeSpan.FromSeconds(5));
        await controller.CheckOnStartupAsync();
        Assert.Equal(1, backend.Checks);
        Assert.Equal(WindowsUpdatePhase.Available, controller.Status.Phase);
        Assert.False(backend.Applied);
        Assert.Equal(0, backend.Downloads);
    }

    [Fact]
    public async Task FailedStartupCanBeRetriedManuallyButNotAutomatically()
    {
        var backend = new Backend { Check = () => throw new IOException() };
        using var controller = new WindowsUpdateController(backend, () => Task.CompletedTask);
        await controller.CheckOnStartupAsync();
        Assert.Equal(WindowsUpdatePhase.Error, controller.Status.Phase);
        await controller.CheckOnStartupAsync();
        Assert.Equal(1, backend.Checks);
        backend.Check = null;
        await controller.CheckAsync();
        Assert.Equal(2, backend.Checks);
        Assert.Equal(WindowsUpdatePhase.Available, controller.Status.Phase);
    }
    private sealed class Backend : IWindowsUpdateBackend
    {
        public int Checks, Downloads;
        public bool Applied;
        public Func<bool>? Locate;
        public Func<Version?>? Check;
        public bool IsInstalled => Locate?.Invoke() ?? true;
        public Version CurrentVersion => new(0, 1, 1);
        public ReleaseNotes? OfferedNotes { get; init; }
        public Task<Version?> CheckAsync() { Checks++; return Task.FromResult(Check is null ? new Version(0, 1, 20) : Check()); }
        public Task DownloadAsync(Version version, Action<int> progress, Action verifying, CancellationToken token) { Downloads++; progress(100); verifying(); return Task.CompletedTask; }
        public void ApplyAndRestart(Version version) => Applied = true;
    }
    private sealed class Download:IFileDownloader
    {
        public string? Fault; public bool Truncated;public List<string> Urls=[];
        private object Release(string tag,string[] assets)=>new{tag_name=tag,draft=false,prerelease=false,assets=assets.Select(name=>new{name,browser_download_url=Fault=="wrong-origin"?"https://evil.example/"+name:$"{WindowsGithubSource.Repository}/releases/download/{tag}/{name}"})};
        public Task<string> DownloadString(string url,IDictionary<string,string>? headers=null,double timeout=30)
        {
            Urls.Add(url);if(url.EndsWith("page=1")||Truncated)return Task.FromResult(JsonSerializer.Serialize(Enumerable.Repeat(Release("v0.1.99",["Agent-android.apk"]),100)));
            return Task.FromResult(JsonSerializer.Serialize(new[]{Release("v0.1.20",["releases.win-x64.json",Fault=="missing-package"?"missing.nupkg":"AgentWindows-0.1.20-win-x64-full.nupkg"]),Release("v0.1.19",["releases.win-x64.json","AgentWindows-0.1.19-win-x64-full.nupkg"])}));
        }
        public Task<byte[]> DownloadBytes(string url,IDictionary<string,string>? headers=null,double timeout=30)
        {
            Urls.Add(url);string entry="""{"PackageId":"AgentWindows","Version":"0.1.20","Type":"Full","FileName":"AgentWindows-0.1.20-win-x64-full.nupkg","Size":100,"SHA256":"ABCD"}""";
            string feed="{\"Assets\":["+entry+(Fault=="duplicate"?","+entry:"")+"]}";if(Fault=="wrong-version")feed=feed.Replace("0.1.20","0.1.21");if(Fault=="empty")feed="{\"Assets\":[]}";return Task.FromResult(Encoding.UTF8.GetBytes(feed));
        }
        public Task DownloadFile(string url,string targetFile,Action<int>? progress=null,IDictionary<string,string>? headers=null,double timeout=30,CancellationToken cancelToken=default){Urls.Add(url);return Task.CompletedTask;}
    }
}
