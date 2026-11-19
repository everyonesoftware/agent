using System.Net;
using System.Text;
using System.Text.Json.Nodes;
using Agent.Core;
using Xunit;
namespace Agent.Core.Tests;
public sealed class SessionTests
{
    [Theory][InlineData("http://example.com")][InlineData("https://user:pass@example.com")][InlineData("https://example.com/?token=secret")][InlineData("https://example.com/#secret")]
    public void RejectUnsafeEndpoints(string endpoint) => Assert.Throws<ArgumentException>(() => ServerEndpoint.Parse(endpoint));
    [Theory][InlineData("https://agent.example")][InlineData("http://127.0.0.1:8080")][InlineData("http://localhost:8080")]
    public void AcceptSecureOrLoopbackEndpoint(string endpoint) => Assert.NotNull(ServerEndpoint.Parse(endpoint));
    [Fact] public async Task LoginPersistsOnlyCookieAndSubsequentReadsUseIt()
    {
        var vault=new TestVault(); var requests=new List<string>();
        using var http=new HttpClient(new Handler(async request => {
            requests.Add(request.RequestUri!.AbsolutePath);
            if(request.RequestUri.AbsolutePath=="/api/auth/login") {
                Assert.Contains("password",await request.Content!.ReadAsStringAsync());
                var response=Json("{\"id\":\"u\",\"username\":\"dan\"}"); response.Headers.Add("Set-Cookie","everyoneagent_session=opaque; HttpOnly; Path=/");return response;
            }
            Assert.Equal("everyoneagent_session=opaque",request.Headers.GetValues("Cookie").Single());
            return Json("{\"id\":\"u\",\"username\":\"dan\"}");
        }));
        using var client=new AgentClient(ServerEndpoint.Parse("https://agent.example"),vault,http);
        await client.SignInAsync("dan","do-not-save",false);
        Assert.Equal("opaque",vault.Token); Assert.DoesNotContain("do-not-save",vault.Token);
        await client.GetAsync("/api/auth/me");Assert.Equal(new[]{"/api/auth/login","/api/auth/me"},requests);
    }
    [Theory][InlineData("0")][InlineData("-1")]
    public async Task SuccessfulLogoutDeletionClearsVaultAndInMemoryCookie(string maxAge)
    {
        var vault=new TestVault{Token="before"};bool loggedOut=false;
        using var http=new HttpClient(new Handler(request=>{
            if(request.RequestUri!.AbsolutePath=="/api/auth/logout")
            {
                Assert.Equal("everyoneagent_session=before",request.Headers.GetValues("Cookie").Single());loggedOut=true;
                var response=Json("{\"logged_out\":true}");
                response.Headers.Add("Set-Cookie","everyoneagent_session=; Path=/; Max-Age="+maxAge+"; HttpOnly; Secure; SameSite=Lax");return Task.FromResult(response);
            }
            if(loggedOut)Assert.False(request.Headers.Contains("Cookie"));
            return Task.FromResult(Json("{}"));
        }));
        using var client=new AgentClient(ServerEndpoint.Parse("https://agent.example"),vault,http);
        await client.RestoreAsync();await client.SignOutAsync();Assert.True(loggedOut);Assert.Null(vault.Token);
        await client.GetAsync("/api/auth/me");
        using var restored=new AgentClient(ServerEndpoint.Parse("https://agent.example"),vault,http);Assert.Null(await restored.RestoreAsync());
    }
    [Fact] public async Task PasswordRotationPersistsNewCookieForCurrentAndRestoredClients()
    {
        var vault=new TestVault{Token="before"};var seen=new List<string>();
        using var http=new HttpClient(new Handler(request=>{
            seen.Add(request.Headers.GetValues("Cookie").Single());
            var response=Json("{}");
            if(request.RequestUri!.AbsolutePath=="/api/auth/password")response.Headers.Add("Set-Cookie","everyoneagent_session=rotated; HttpOnly; Path=/");
            return Task.FromResult(response);
        }));
        using(var client=new AgentClient(ServerEndpoint.Parse("https://agent.example"),vault,http))
        {
            await client.RestoreAsync();await client.PostAsync("/api/auth/password",new{current_password="transient",new_password="also-transient"});
            Assert.Equal("rotated",vault.Token);await client.GetAsync("/api/auth/me");
        }
        using(var restored=new AgentClient(ServerEndpoint.Parse("https://agent.example"),vault,http))await restored.RestoreAsync();
        Assert.Equal(new[]{"everyoneagent_session=before","everyoneagent_session=before","everyoneagent_session=rotated","everyoneagent_session=rotated"},seen);
    }
    [Theory][InlineData("/api/chat","reauthorize")][InlineData("/api/auth/password","")][InlineData("/api/auth/disconnect","")][InlineData("/api/auth/profiles/active","")]
    public async Task ProviderReauthorizationAndBadConfirmationDoNotExpireAgentSession(string path,string action)
    {
        var vault=new TestVault{Token="retained"};bool expired=false;int calls=0;
        using var http=new HttpClient(new Handler(request=>{
            Assert.Equal("everyoneagent_session=retained",request.Headers.GetValues("Cookie").Single());calls++;
            if(request.RequestUri!.AbsolutePath==path)return Task.FromResult(new HttpResponseMessage(HttpStatusCode.Unauthorized){Content=new StringContent("{\"action\":\""+action+"\",\"error\":\"sensitive-provider-detail\"}")});
            return Task.FromResult(Json("{}"));
        }));
        using var client=new AgentClient(ServerEndpoint.Parse("https://agent.example"),vault,http);client.SessionExpired+=()=>expired=true;
        await client.RestoreAsync();var error=await Assert.ThrowsAsync<AgentException>(()=>client.PostAsync(path,new{}));
        Assert.IsNotType<SessionExpiredException>(error);Assert.DoesNotContain("sensitive-provider-detail",error.Message);
        Assert.False(expired);Assert.Equal("retained",vault.Token);await client.GetAsync("/api/auth/me");Assert.Equal(3,calls);
    }
    [Theory][InlineData("/api/auth/password")][InlineData("/api/auth/disconnect")][InlineData("/api/auth/profiles/active")]
    public async Task ExpiredSessionOnConfirmationRouteStillClearsVault(string path)
    {
        var vault=new TestVault{Token="expired"};bool expired=false;
        using var http=new HttpClient(new Handler(request=>Task.FromResult(request.RequestUri!.AbsolutePath=="/api/auth/me"?Json("{}"):new HttpResponseMessage(HttpStatusCode.Unauthorized){Content=new StringContent("{\"error\":\"Sign in to continue.\"}")})));
        using var client=new AgentClient(ServerEndpoint.Parse("https://agent.example"),vault,http);client.SessionExpired+=()=>expired=true;
        await client.RestoreAsync();await Assert.ThrowsAsync<SessionExpiredException>(()=>client.PostAsync(path,new{}));
        Assert.True(expired);Assert.Null(vault.Token);
    }
    [Fact] public async Task RejectRedirectRatherThanForwardCookie()
    {
        using var http=new HttpClient(new Handler(_=>Task.FromResult(new HttpResponseMessage(HttpStatusCode.Redirect){Headers={Location=new Uri("https://github.com/")}})));
        using var client=new AgentClient(ServerEndpoint.Parse("https://agent.example"),new TestVault(),http);
        await Assert.ThrowsAsync<AgentException>(()=>client.GetAsync("/api/auth/me"));
        await Assert.ThrowsAsync<ArgumentException>(()=>client.GetAsync("https://github.com/api/auth/me"));
    }
    [Fact] public async Task ExpiryClearsVault()
    {
        var vault=new TestVault{Token="old"};using var http=new HttpClient(new Handler(_=>Task.FromResult(new HttpResponseMessage(HttpStatusCode.Unauthorized))));
        using var client=new AgentClient(ServerEndpoint.Parse("https://agent.example"),vault,http);
        await Assert.ThrowsAsync<SessionExpiredException>(()=>client.RestoreAsync());Assert.Null(vault.Token);
    }
    internal static HttpResponseMessage Json(string json)=>new(HttpStatusCode.OK){Content=new StringContent(json,Encoding.UTF8,"application/json")};
    internal sealed class Handler(Func<HttpRequestMessage,Task<HttpResponseMessage>> respond):HttpMessageHandler { protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage r,CancellationToken c)=>respond(r); }
    internal sealed class TestVault:ISessionVault {public string? Token;public Task<string?> ReadAsync(string endpoint)=>Task.FromResult(Token);public Task SaveAsync(string endpoint,string token){Token=token;return Task.CompletedTask;}public Task RemoveAsync(string endpoint){Token=null;return Task.CompletedTask;}}
}
