using System.Text.Json.Nodes;

namespace Agent.Core;

public sealed record SavedConversation(string Id, string Title, long UpdatedAt);
public sealed record ConversationHistoryRow(string Id, string Title, string Group, DateTime? LocalDate);

public static class ConversationHistory
{
    public static IReadOnlyList<SavedConversation> Read(JsonNode result) => result.Items("conversations")
        .Select(item => new SavedConversation(item.Text("id"), item.Text("title", "Conversation"), item["updated_at"]?.GetValue<long>() ?? 0))
        .Where(item => item.Id.Length != 0).OrderByDescending(item => item.UpdatedAt).ToArray();

    public static IEnumerable<ConversationHistoryRow> Filter(IEnumerable<SavedConversation> items, string query, DateTimeOffset now, TimeZoneInfo zone)
    {
        query = query.Trim();
        var today = TimeZoneInfo.ConvertTime(now, zone).Date;
        foreach (var item in items)
        {
            string title = string.IsNullOrWhiteSpace(item.Title) ? "Conversation" : item.Title;
            if (query.Length != 0 && !title.Contains(query, StringComparison.CurrentCultureIgnoreCase)) continue;
            DateTime? date = item.UpdatedAt is > 0 and <= 253402300799
                ? TimeZoneInfo.ConvertTime(DateTimeOffset.FromUnixTimeSeconds(item.UpdatedAt), zone).Date : null;
            string group = date >= today ? "Today" : date == today.AddDays(-1) ? "Yesterday"
                : date >= today.AddDays(-7) ? "Previous 7 days" : "Earlier";
            yield return new(item.Id, title, group, date);
        }
    }
}
