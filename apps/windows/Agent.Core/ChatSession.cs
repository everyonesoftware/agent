using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.Json.Serialization;
namespace Agent.Core;
public sealed record ChatMessage([property:JsonPropertyName("role")]string Role,[property:JsonPropertyName("content")]string Content);
public sealed record ChatRequest(
    [property:JsonPropertyName("messages")]IReadOnlyList<ChatMessage> Messages,
    [property:JsonPropertyName("model")]string Model,
    [property:JsonPropertyName("conversation_id"),JsonIgnore(Condition=JsonIgnoreCondition.WhenWritingNull)]string? ConversationId,
    [property:JsonPropertyName("omitted_messages")]int OmittedMessages,
    [property:JsonPropertyName("run_id")]string RunId,
    [property:JsonPropertyName("research_task_id"),JsonIgnore(Condition=JsonIgnoreCondition.WhenWritingNull)]string? ResearchTaskId = null)
{
    public ChatRequest WithResearch(string taskId) => !string.IsNullOrWhiteSpace(taskId) ? this with { ResearchTaskId=taskId } : throw new ArgumentException("A task-scoped grant is required.");
}
public sealed class ChatSession
{
    public List<ChatMessage> Messages { get; }=[];
    public string? ConversationId { get; private set; }
    public int OmittedMessages { get; private set; }
    public long Revision { get; private set; }
    public void Reset(){Revision++;Messages.Clear();ConversationId=null;OmittedMessages=0;}
    public void Load(string id,JsonNode result)
    {
        Reset();ConversationId=id;OmittedMessages=result["omitted_messages"]?.GetValue<int>()??0;
        Messages.AddRange(result.Items("messages").Select(m=>new ChatMessage(m.Text("role"),m.Text("content"))));
    }
    public ChatRequest CreateRequest(string model)
    {
        if(string.IsNullOrWhiteSpace(model)||model.Length>256||Messages.Count==0)throw new ArgumentException("Select a model and enter a message.");
        var selected=Messages.TakeLast(40).ToArray();
        if(selected.Any(m=>m.Role is not ("user" or "assistant" or "system") || Encoding.UTF8.GetByteCount(m.Content)>8192 || string.IsNullOrWhiteSpace(m.Content)))throw new ArgumentException("Each message must contain at most 8 KiB of text.");
        var request=new ChatRequest(selected,model,ConversationId,OmittedMessages+Messages.Count-selected.Length,Guid.NewGuid().ToString());
        if(JsonSerializer.SerializeToUtf8Bytes(request).Length>64*1024)throw new ArgumentException("This conversation exceeds the 64 KiB request limit. Start a new conversation.");
        return request;
    }
    public bool AcceptReply(long revision,JsonNode reply)
    {
        if(revision!=Revision)return false;
        if(string.IsNullOrEmpty(reply.Text("reply")))throw new AgentException("The server returned no reply. Check history before resending.");
        ConversationId=reply.Text("conversation_id",ConversationId??"");Messages.Add(new("assistant",reply.Text("reply")));return true;
    }
}
public static class ClientPresentation
{
    public static IReadOnlyList<string> SettingsOrder {get;}=["Updates","Connection","ChatGPT Connection","Web Research","Memory","Reset Password","Sign Out"];
    public static double? Remaining(double? used)=>used is >=0 and <=100 ? Math.Round(100-used.Value,1) : null;
    public static Uri VerificationUri(string value)
    {
        if(!Uri.TryCreate(value,UriKind.Absolute,out var uri)||uri.Scheme!="https"||!string.IsNullOrEmpty(uri.UserInfo)||uri.Port!=443||!(uri.Host=="auth.openai.com"||uri.Host=="chatgpt.com"||uri.Host=="platform.openai.com"))throw new AgentException("The server returned an untrusted verification page.");return uri;
    }
}
