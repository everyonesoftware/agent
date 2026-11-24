using System.Text.Json.Nodes;

namespace Agent.Core.Tests;

public sealed class ConversationHistoryTests
{
    private static readonly JsonNode Result = JsonNode.Parse("""
        {"conversations":[
          {"id":"older","title":"Tesla news","updated_at":1761955200},
          {"id":"today","title":"Tesla follow-up","updated_at":1763942400},
          {"id":"yesterday","title":"NBA games","updated_at":1763856000},
          {"id":"week","title":"Other topic","updated_at":1763337600},
          {"id":"undated"},
          {"title":"No ID","updated_at":1763942400}
        ]}
        """)!;
    private static readonly TimeZoneInfo Utc = TimeZoneInfo.Utc;
    private static readonly DateTimeOffset Now = new(2025, 11, 24, 12, 0, 0, TimeSpan.Zero);

    [Xunit.Fact]
    public void FiltersSavedTitlesInNewestFirstOrder()
    {
        var rows = ConversationHistory.Filter(ConversationHistory.Read(Result), " tEsLa ", Now, Utc).ToArray();
        Xunit.Assert.Equal(new[] { "today", "older" }, rows.Select(row => row.Id));
        Xunit.Assert.Empty(ConversationHistory.Filter(ConversationHistory.Read(Result), "not saved", Now, Utc));
    }

    [Xunit.Fact]
    public void GroupsByLocalCalendarDateAndKeepsUndatedConversations()
    {
        var rows = ConversationHistory.Filter(ConversationHistory.Read(Result), "", Now, Utc).ToArray();
        Xunit.Assert.Equal(new[] { "Today", "Yesterday", "Previous 7 days", "Earlier", "Earlier" }, rows.Select(row => row.Group));
        Xunit.Assert.Null(rows[^1].LocalDate);
        Xunit.Assert.Equal("Conversation", rows[^1].Title);
        var west = TimeZoneInfo.CreateCustomTimeZone("Test West", TimeSpan.FromHours(-8), "Test West", "Test West");
        Xunit.Assert.Equal("Yesterday", ConversationHistory.Filter(ConversationHistory.Read(Result), "follow-up", Now, west).Single().Group);
    }
}
