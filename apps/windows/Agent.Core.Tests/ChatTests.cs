using System.Text.Json;
using System.Text.Json.Nodes;
using Agent.Core;
using Xunit;
namespace Agent.Core.Tests;
public sealed class ChatTests
{
    [Fact] public void OrdinaryChatIsToolFreeAndBounded()
    {
        var state=new ChatSession();state.Messages.Add(new("user",new string('a',8193)));
        Assert.Throws<ArgumentException>(()=>state.CreateRequest("model"));
        state.Messages.Clear();for(int i=0;i<44;i++)state.Messages.Add(new(i%2==0?"user":"assistant","message"));
        var request=state.CreateRequest("model");var json=JsonSerializer.Serialize(request);
        Assert.DoesNotContain("research_task_id",json);Assert.Equal(40,request.Messages.Count);Assert.Equal(4,request.OmittedMessages);
    }
    [Fact] public void ReplyOnlyPublishesIntoInitiatingConversation()
    {
        var state=new ChatSession();long lease=state.Revision;state.Reset();
        Assert.False(state.AcceptReply(lease,JsonNode.Parse("{\"reply\":\"late\",\"conversation_id\":\"old\"}")!));Assert.Empty(state.Messages);
    }
    [Fact] public void ResearchGrantBindsExactPayloadAndIsNeverAutomatic()
    {
        var state=new ChatSession();state.Messages.Add(new("user","Read public sources"));
        var request=state.CreateRequest("model");Assert.Null(request.ResearchTaskId);
        Assert.Throws<ArgumentException>(()=>request.WithResearch(""));
        Assert.Equal("task",request.WithResearch("task").ResearchTaskId);Assert.Null(request.ResearchTaskId);
    }
    [Fact] public void NativeSettingsFollowApprovedOrder()=>Assert.Equal(new[]{"Updates","Connection","ChatGPT Connection","Web Research","Memory","Reset Password","Sign Out"},ClientPresentation.SettingsOrder);
    [Fact] public void AllowanceNeverInventsMissingValues()
    {
        Assert.Null(ClientPresentation.Remaining(null));Assert.Null(ClientPresentation.Remaining(-1));Assert.Null(ClientPresentation.Remaining(101));Assert.Equal(82d,ClientPresentation.Remaining(18));
    }
}
