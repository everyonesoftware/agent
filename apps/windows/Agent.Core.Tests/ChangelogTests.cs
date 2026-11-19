using System.Net;
using System.Text.Json;
using Agent.Core;
using Xunit;
namespace Agent.Core.Tests;
public sealed class ChangelogTests
{
    [Fact] public void GroupsRealSubjectsWithoutOtherPlatformOrMechanicalPrefixes()
    {
        var notes=ReleaseNotes.Group("a","b",[new("1","[verified] fix(auth): Repair sessions"),new("2","feat(windows): Add native client"),new("3","docs: Update docs"),new("4","fix(android): Repair phone"),new("5","Improve shared model handling")]);
        Assert.Equal(new[]{"Repair sessions"},notes.Fixes);Assert.Equal(new[]{"Add native client","Improve shared model handling"},notes.Improvements);Assert.Equal(5,notes.Commits.Count);Assert.Equal("a",notes.From);
    }
    [Fact] public async Task NotesRequireExactPublishedSourceAndCompleteCompareRange()
    {
        string from=new('a',40),to=new('b',40);int pages=0;
        using var http=new HttpClient(new SessionTests.Handler(r=>{
            string path=r.RequestUri!.AbsolutePath;
            if(path.EndsWith("agent-windows-release.json"))return Task.FromResult(SessionTests.Json(JsonSerializer.Serialize(new{schemaVersion=1,platform="windows",version="0.1.20",commit=to})));
            pages++;return Task.FromResult(SessionTests.Json(JsonSerializer.Serialize(new{status="ahead",total_commits=101,base_commit=new{sha=from},commits=Enumerable.Range(0,pages==1?100:1).Select(i=>new{sha=(pages*100+i).ToString("x40"),commit=new{message="fix: Repair native state"}})})));
        }));
        var notes=await new ChangelogClient(http).LoadAsync("0.1.20",from);Assert.Equal(to,notes.To);Assert.Equal(101,notes.Commits.Count);Assert.Equal(2,pages);
    }
    [Theory]
    [InlineData("release-assets.githubusercontent.com")]
    [InlineData("objects.githubusercontent.com")]
    [InlineData("github-releases.githubusercontent.com")]
    public async Task ManifestFollowsAllowlistedAssetRedirectWithoutCredentials(string host)
    {
        string from=new('a',40),to=new('b',40);var seen=new List<string>();
        using var http=new HttpClient(new SessionTests.Handler(r=>{
            Assert.False(r.Headers.Contains("Cookie"));Assert.Null(r.Headers.Authorization);seen.Add(r.RequestUri!.Host);
            if(r.RequestUri.Host=="github.com")return Task.FromResult(new HttpResponseMessage(HttpStatusCode.Redirect){Headers={Location=new Uri($"https://{host}/github-production-release-asset/123/456?signature=test-only")}});
            if(r.RequestUri.Host==host)return Task.FromResult(SessionTests.Json(JsonSerializer.Serialize(new{schemaVersion=1,platform="windows",version="0.1.20",commit=to})));
            return Task.FromResult(SessionTests.Json(JsonSerializer.Serialize(new{status="ahead",total_commits=1,base_commit=new{sha=from},commits=new[]{new{sha=to,commit=new{message="fix: Repair sessions"}}}})));
        }));
        using var client=new ChangelogClient(http);var notes=await client.LoadAsync("0.1.20",from);
        Assert.Equal(new[]{"github.com",host,"api.github.com"},seen);Assert.Equal(to,notes.To);Assert.Single(notes.Fixes);
    }
    [Theory]
    [InlineData("https://evil.example/asset")]
    [InlineData("https://release-assets.githubusercontent.com.evil.example/asset")]
    [InlineData("http://release-assets.githubusercontent.com/asset")]
    [InlineData("https://user:password@release-assets.githubusercontent.com/asset")]
    [InlineData("https://release-assets.githubusercontent.com:444/asset")]
    [InlineData("https://api.github.com/asset")]
    public async Task ManifestRejectsHostileRedirectBeforeSending(string location)
    {
        int calls=0;using var http=new HttpClient(new SessionTests.Handler(r=>{
            calls++;return Task.FromResult(new HttpResponseMessage(HttpStatusCode.Redirect){Headers={Location=new Uri(location)}});
        }));
        using var client=new ChangelogClient(http);
        await Assert.ThrowsAsync<InvalidDataException>(()=>client.LoadAsync("0.1.20",new('a',40)));Assert.Equal(1,calls);
    }
    [Fact] public async Task ManifestRedirectLoopIsBounded()
    {
        int calls=0;using var http=new HttpClient(new SessionTests.Handler(r=>{
            calls++;return Task.FromResult(new HttpResponseMessage(HttpStatusCode.Redirect){Headers={Location=new Uri("https://release-assets.githubusercontent.com/github-production-release-asset/123/loop")}});
        }));
        using var client=new ChangelogClient(http);
        await Assert.ThrowsAsync<InvalidDataException>(()=>client.LoadAsync("0.1.20",new('a',40)));Assert.Equal(6,calls);
    }
    [Fact] public async Task CompareApiRedirectIsStillRejected()
    {
        int calls=0;using var http=new HttpClient(new SessionTests.Handler(r=>{
            calls++;if(calls==1)return Task.FromResult(SessionTests.Json(JsonSerializer.Serialize(new{schemaVersion=1,platform="windows",version="0.1.20",commit=new string('b',40)})));
            return Task.FromResult(new HttpResponseMessage(HttpStatusCode.Redirect){Headers={Location=new Uri("https://release-assets.githubusercontent.com/github-production-release-asset/123/compare")}});
        }));
        using var client=new ChangelogClient(http);
        await Assert.ThrowsAsync<InvalidDataException>(()=>client.LoadAsync("0.1.20",new('a',40)));Assert.Equal(2,calls);
    }
    [Fact] public async Task MissingInstalledSourceDoesNotInventNotes()
    {
        using var http=new HttpClient(new SessionTests.Handler(_=>throw new Exception("Network must not be used")));
        await Assert.ThrowsAsync<InvalidDataException>(()=>new ChangelogClient(http).LoadAsync("0.1.20","unknown"));
    }
}
