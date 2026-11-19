using Agent.Core;
using Xunit;
namespace Agent.Core.Tests;
public sealed class AccountViewTests
{
    [Fact] public void TransitionClearsAllPasswordDraftsAndInvalidatesEverySettingsSection()
    {
        var state=new AccountViewState();
        var inputs=new Dictionary<string,string>{{"Password","login"},{"BraveKey","old-key"},{"CurrentPassword","old-password"},{"NewPassword","draft"},{"ConfirmPassword","draft"}};
        foreach(var section in Enum.GetValues<SettingsSection>())state.MarkLoaded(section);
        bool clearedPresentation=false;
        state.Reset(name=>inputs[name]="",()=>clearedPresentation=true);
        Assert.All(inputs.Values,value=>Assert.Equal("",value));Assert.True(clearedPresentation);
        foreach(var section in Enum.GetValues<SettingsSection>())Assert.Throws<AgentException>(()=>state.Require(section));
        state.MarkLoaded(SettingsSection.Research);state.Require(SettingsSection.Research);
        Assert.Throws<AgentException>(()=>state.Require(SettingsSection.Memory));
    }
    [Fact] public async Task PriorUnsavedKeyCannotBeSubmittedToFreshDestination()
    {
        var state=new AccountViewState();var inputs=new Dictionary<string,string>();
        inputs["BraveKey"]="prior-account-secret";state.MarkLoaded(SettingsSection.Research);
        state.Reset(name=>inputs[name]="",()=>{});
        bool sent=false;
        using var http=new HttpClient(new SessionTests.Handler(async request=>{
            sent=true;string payload=await request.Content!.ReadAsStringAsync();
            Assert.Equal("new.example",request.RequestUri!.Host);Assert.DoesNotContain("prior-account-secret",payload);
            Assert.Contains("fresh-key",payload);return SessionTests.Json("{}");
        }));
        using var destination=new AgentClient(ServerEndpoint.Parse("https://new.example"),new SessionTests.TestVault(),http);
        async Task Submit(){state.Require(SettingsSection.Research);await destination.PostAsync("/api/research/settings/verify",new{api_key=inputs["BraveKey"]});}
        await Assert.ThrowsAsync<AgentException>(Submit);Assert.False(sent);Assert.Equal("",inputs["BraveKey"]);
        state.MarkLoaded(SettingsSection.Research);inputs["BraveKey"]="fresh-key";await Submit();Assert.True(sent);
    }
}
