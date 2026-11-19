using Agent.Core;
using Windows.Security.Credentials;
namespace Agent.App;
/// <summary>Only origin-bound opaque Agent cookies enter PasswordVault; passwords and provider keys never do.</summary>
public sealed class SessionVault:ISessionVault
{
    private const string Resource="EveryoneSoftware.Agent.Session";
    public Task<string?> ReadAsync(string endpoint)
    {
        try{var credential=new PasswordVault().Retrieve(Resource,endpoint);credential.RetrievePassword();return Task.FromResult<string?>(credential.Password);}
        catch(Exception e) when(e.HResult==unchecked((int)0x80070490)){return Task.FromResult<string?>(null);}
    }
    public Task SaveAsync(string endpoint,string token){new PasswordVault().Add(new PasswordCredential(Resource,endpoint,token));return Task.CompletedTask;}
    public Task RemoveAsync(string endpoint)
    {
        var vault=new PasswordVault();try{vault.Remove(vault.Retrieve(Resource,endpoint));}catch(Exception e) when(e.HResult==unchecked((int)0x80070490)){}return Task.CompletedTask;
    }
}
internal static class LocalSettings
{
    private static readonly string DirectoryPath=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"EveryoneSoftware","Agent");
    private static readonly string EndpointPath=Path.Combine(DirectoryPath,"endpoint.txt");
    public static string? ReadEndpoint()=>File.Exists(EndpointPath)?File.ReadAllText(EndpointPath):null;
    public static void SaveEndpoint(Uri endpoint){Directory.CreateDirectory(DirectoryPath);File.WriteAllText(EndpointPath,endpoint.AbsoluteUri);}
}
