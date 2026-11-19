using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
namespace Agent.Core;
public sealed record SourceCommit(string Sha,string Subject);
public sealed record ReleaseNotes(string From,string To,IReadOnlyList<SourceCommit> Commits,IReadOnlyList<string> Improvements,IReadOnlyList<string> Fixes)
{
    public static ReleaseNotes Group(string from,string to,IReadOnlyList<SourceCommit> commits)
    {
        var improvements=new List<string>();var fixes=new List<string>();
        foreach(var commit in commits)
        {
            string subject=Regex.Replace(commit.Subject.Split('\n')[0],@"^(\[verified\]\s*)+","",RegexOptions.IgnoreCase);
            if(Regex.IsMatch(subject,@"^(docs|ci|build\(android\)|chore\(ci\))([(:!])",RegexOptions.IgnoreCase)||Regex.IsMatch(subject,@"^\w+\((android|web|container)\)!?:",RegexOptions.IgnoreCase))continue;
            bool fix=Regex.IsMatch(subject,@"^(fix|bugfix|revert)(\([^)]*\))?!?:",RegexOptions.IgnoreCase);
            string bullet=Regex.Replace(subject,@"^\w+(\([^)]*\))?!?:\s*","");
            (fix?fixes:improvements).Add(bullet);
        }
        return new(from,to,commits,improvements,fixes);
    }
}
/// <summary>Independent unauthenticated GitHub transport; release descriptions are never changelogs.</summary>
public sealed class ChangelogClient : IDisposable
{
    private readonly HttpClient http;private readonly bool owns;
    public ChangelogClient(HttpClient? transport=null){owns=transport is null;http=transport??new HttpClient(new HttpClientHandler{AllowAutoRedirect=false,UseCookies=false}){Timeout=TimeSpan.FromSeconds(30)};}
    private static bool IsReleaseAsset(Uri uri) => uri.Scheme==Uri.UriSchemeHttps && uri.IsDefaultPort && uri.UserInfo=="" && uri.Fragment=="" &&
        ((uri.Host=="github.com" && uri.AbsolutePath.StartsWith("/everyonesoftware/agent/releases/download/",StringComparison.Ordinal)) ||
         (uri.Host is "release-assets.githubusercontent.com" or "objects.githubusercontent.com" or "github-releases.githubusercontent.com" && uri.AbsolutePath.StartsWith("/github-production-release-asset/",StringComparison.Ordinal)));
    private async Task<JsonNode> ReadAsync(string url,CancellationToken token,bool releaseAsset=false)
    {
        var target=new Uri(url);
        for(int redirects=0;;redirects++)
        {
            using var request=new HttpRequestMessage(HttpMethod.Get,target);request.Headers.UserAgent.ParseAdd("AgentWindows/0.1");request.Headers.Accept.ParseAdd("application/vnd.github+json");
            using var response=await http.SendAsync(request,token);
            if((int)response.StatusCode is >=300 and <400)
            {
                var location=response.Headers.Location;
                if(!releaseAsset || redirects>=5 || location is null || !Uri.TryCreate(target,location,out var next) || !IsReleaseAsset(next))
                    throw new InvalidDataException("Untrusted or excessive source evidence redirect.");
                target=next;continue;
            }
            response.EnsureSuccessStatusCode();return JsonNode.Parse(await response.Content.ReadAsStringAsync(token))??throw new InvalidDataException("Missing source evidence.");
        }
    }
    public async Task<ReleaseNotes> LoadAsync(string offeredVersion,string installedCommit,CancellationToken token=default)
    {
        if(!Regex.IsMatch(installedCommit,"^[a-f0-9]{40}$")||!Regex.IsMatch(offeredVersion,@"^0\.1\.[1-9][0-9]*$"))throw new InvalidDataException("Installed source identity is unavailable.");
        var manifest=await ReadAsync($"https://github.com/everyonesoftware/agent/releases/download/v{offeredVersion}/agent-windows-release.json",token,releaseAsset:true);
        string offered=manifest.Text("commit");
        if(manifest["schemaVersion"]?.GetValue<int>()!=1||manifest.Text("platform")!="windows"||manifest.Text("version")!=offeredVersion||!Regex.IsMatch(offered,"^[a-f0-9]{40}$"))throw new InvalidDataException("Offered source identity is unavailable.");
        var commits=new List<SourceCommit>();int total=-1;
        for(int page=1;page<=10;page++)
        {
            var result=await ReadAsync($"https://api.github.com/repos/everyonesoftware/agent/compare/{installedCommit}...{offered}?per_page=100&page={page}",token);
            if(result["base_commit"].Text("sha")!=installedCommit||result.Text("status") is not ("ahead" or "identical"))throw new InvalidDataException("The source range could not be verified.");
            int expected=result["total_commits"]?.GetValue<int>()??throw new InvalidDataException("Missing history count.");
            if(total!=-1&&total!=expected)throw new InvalidDataException("History changed during pagination.");total=expected;
            var batch=result.Items("commits").Select(c=>new SourceCommit(c.Text("sha"),c["commit"].Text("message"))).ToArray();commits.AddRange(batch);
            if(batch.Length<100){if(commits.Count!=total||commits.Select(c=>c.Sha).Distinct().Count()!=total)throw new InvalidDataException("History was incomplete.");return ReleaseNotes.Group(installedCommit,offered,commits);}
        }
        throw new InvalidDataException("The complete changelog exceeds the safe paging limit.");
    }
    public void Dispose(){if(owns)http.Dispose();}
}
