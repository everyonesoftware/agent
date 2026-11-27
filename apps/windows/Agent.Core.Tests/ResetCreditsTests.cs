using System.Net;
using System.Text.Json.Nodes;
using Agent.Core;
using Xunit;

namespace Agent.Core.Tests;

public sealed class ResetCreditsTests
{
    private const long Now = 1900000099;
    private const string Status = """
        {"state":"connected","available":true,"profile_id":"p","connection_id":"c","snapshot":{"stale":false},"resets":{
          "available_count":3,"details_complete":false,
          "expirations":[{"count":1,"expires_at":1900000200},{"count":1,"expires_at":null},{"count":1,"expires_at":1900000100}],
          "next":{"credit_id":"earliest","expires_at":1900000100},
          "history":[{"kind":"used","at":1800000100},{"kind":"expired","at":1800000200}]}}
        """;

    [Fact]
    public void PartialBalanceKeepsKnownExpirationsAndNewestFirstHistory()
    {
        var result = Assert.IsType<ResetCredits>(ResetCredits.Read(JsonNode.Parse(Status), "p", "c"));
        Assert.Equal(3, result.AvailableCount);
        Assert.False(result.DetailsComplete);
        Assert.Equal(new long?[] { 1900000100, 1900000200, null }, result.Expirations.Select(x => x.ExpiresAt));
        Assert.Equal(new[] { "expired", "used" }, result.History.Select(x => x.Kind));
        Assert.Null(result.EligibleOffer(Now));
    }

    [Fact]
    public void CompleteBalanceUsesOnlyCurrentEarliestCredit()
    {
        var status = JsonNode.Parse(Status.Replace("\"details_complete\":false", "\"details_complete\":true"));
        var result = Assert.IsType<ResetCredits>(ResetCredits.Read(status, "p", "c"));
        var offer = Assert.IsType<ResetOffer>(result.EligibleOffer(Now));
        Assert.Equal("earliest", offer.Credit.Id);
        Assert.Equal(3, offer.AvailableCount);
        Assert.Null(result.EligibleOffer(1900000100));
        Assert.Null((result with { Next = new("later", 1900000200) }).EligibleOffer(Now));
        Assert.Null((result with { HistoryUnavailable = true }).EligibleOffer(Now));
    }

    [Fact]
    public void ConfirmationIdentityChangesWithAnyOfferField()
    {
        var offer = ReadOffer(CompleteStatus());
        Assert.Equal(offer, ReadOffer(CompleteStatus()));
        Assert.NotEqual(offer, offer with { AvailableCount = offer.AvailableCount + 1 });
        Assert.NotEqual(offer, offer with { Credit = offer.Credit with { Id = "different" } });
        Assert.NotEqual(offer, offer with { Credit = offer.Credit with { ExpiresAt = offer.Credit.ExpiresAt + 1 } });
        var status = CompleteStatus();
        status["resets"]!["next"]!["pending_attempt_id"] = "abcdef12-1000-4000-9000-123456789abc";
        Assert.NotEqual(offer, ReadOffer(status));
    }

    [Theory]
    [InlineData("resets", "legacy")]
    [InlineData("false", "true", "\"stale\":")]
    [InlineData("\"p\"", "\"other\"")]
    [InlineData("\"c\"", "\"changed\"")]
    [InlineData("\"connected\"", "\"unavailable\"")]
    [InlineData("\"available\":true", "\"available\":false")]
    [InlineData("\"available\":true", "\"available\":true,\"error\":\"stale provider data\"")]
    [InlineData("1900000100", "9223372036854775807")]
    [InlineData("\"available_count\":3", "\"available_count\":-1")]
    public void AbsentStaleWrongScopeOrInvalidDataIsNotAZeroBalance(string from, string to, string prefix = "") =>
        Assert.Null(ResetCredits.Read(JsonNode.Parse(Status.Replace(prefix + from, prefix + to)), "p", "c"));

    [Fact]
    public void VerifiedEmptyBalanceHasNoExpiryOrActionButKeepsHistoryState()
    {
        var status = JsonNode.Parse(Status)!;
        status["resets"] = JsonNode.Parse("""
            {"available_count":0,"expirations":[],"details_complete":true,"history":[],"history_unavailable":true}
            """);
        var result = Assert.IsType<ResetCredits>(ResetCredits.Read(status, "p", "c"));
        Assert.Equal(0, result.AvailableCount);
        Assert.Empty(result.Expirations);
        Assert.Null(result.EligibleOffer(1900000000));
        Assert.True(result.HistoryUnavailable);
    }

    [Theory]
    [InlineData("reset")][InlineData("alreadyRedeemed")][InlineData("nothingToReset")][InlineData("noCredit")]
    public async Task ConsumeSendsExactConfirmedTargetOnceAndReturnsAuthoritativeStatus(string outcome)
    {
        var handler = new Capture(async request =>
        {
            Assert.Equal(HttpMethod.Post, request.Method);
            Assert.Equal("/api/usage/resets/consume", request.RequestUri!.AbsolutePath);
            var body = JsonNode.Parse(await request.Content!.ReadAsStringAsync())!.AsObject();
            Assert.Equal(6, body.Count);
            Assert.Equal(3, (int?)body["confirmed_count"]);
            Assert.Equal(1900000100L, (long?)body["confirmed_expires_at"]);
            Assert.Equal("p", (string?)body["profile_id"]);
            Assert.Equal("c", (string?)body["connection_id"]);
            Assert.Equal("earliest", (string?)body["credit_id"]);
            Assert.True(Guid.TryParseExact((string?)body["attempt_id"], "D", out var attempt));
            Assert.NotEqual(Guid.Empty, attempt);
            return new HttpResponseMessage(HttpStatusCode.OK) { Content = new StringContent($$"""{"outcome":"{{outcome}}","status":{{Status}}}""") };
        });
        using var http = new HttpClient(handler);
        using var client = new AgentClient(new Uri("https://agent.example"), new Vault(), http);
        var result = await ResetCredits.ConsumeAsync(client, "p", "c", ReadOffer(CompleteStatus()), default);
        Assert.Equal(outcome, result.Outcome);
        Assert.Equal(3, ResetCredits.Read(result.Status, "p", "c")!.AvailableCount);
        Assert.Equal(1, handler.Calls);
    }

    [Theory]
    [InlineData("{\"outcome\":\"reset\",\"status\":{\"profile_id\":\"other\",\"connection_id\":\"c\"}}")]
    [InlineData("{\"outcome\":\"reset\"}")]
    [InlineData("{\"outcome\":\"unexpected\",\"status\":{\"profile_id\":\"p\",\"connection_id\":\"c\"}}")]
    [InlineData(null)]
    public async Task UnverifiedAcknowledgementDoesNotReplay(string? reply)
    {
        var handler = new Capture(_ => reply is null ? throw new HttpRequestException("Response lost")
            : Task.FromResult(new HttpResponseMessage(HttpStatusCode.OK) { Content = new StringContent(reply) }));
        using var http = new HttpClient(handler);
        using var client = new AgentClient(new Uri("https://agent.example"), new Vault(), http);
        var error = await Record.ExceptionAsync(() => ResetCredits.ConsumeAsync(client, "p", "c", ReadOffer(CompleteStatus()), default));
        Assert.True(error is AgentException or HttpRequestException);
        Assert.Equal(1, handler.Calls);
    }

    [Fact]
    public async Task LostReplyRecoversOnlyAfterFreshReadAndExplicitConfirmationOnReconstructedClient()
    {
        const string path = "/api/usage/status?profile_id=p";
        var status = CompleteStatus();
        status["resets"] = JsonNode.Parse("""
            {"available_count":1,"details_complete":true,"expirations":[{"count":1,"expires_at":null}],
             "next":{"credit_id":"undated","expires_at":null},"history":[]}
            """);
        var final = CompleteStatus();
        final["resets"] = JsonNode.Parse("""
            {"available_count":0,"details_complete":true,"expirations":[],"history":[{"kind":"used","at":1900000099}]}
            """);

        string? reserved = null;
        var traffic = new List<string>();
        using var handler = new Capture(async request =>
        {
            traffic.Add(request.Method.Method);
            if (request.Method == HttpMethod.Get)
            {
                Assert.Equal(path, request.RequestUri!.PathAndQuery);
                return new(HttpStatusCode.OK) { Content = new StringContent(status.ToJsonString()) };
            }

            Assert.Equal(HttpMethod.Post, request.Method);
            Assert.Equal("/api/usage/resets/consume", request.RequestUri!.AbsolutePath);
            var body = JsonNode.Parse(await request.Content!.ReadAsStringAsync())!.AsObject();
            Assert.Equal("p", (string?)body["profile_id"]);
            Assert.Equal("c", (string?)body["connection_id"]);
            Assert.Equal("undated", (string?)body["credit_id"]);
            Assert.Equal(1, (int?)body["confirmed_count"]);
            Assert.True(body.ContainsKey("confirmed_expires_at"));
            Assert.Null(body["confirmed_expires_at"]);
            var attempt = Assert.IsType<string>((string?)body["attempt_id"]);
            if (reserved is null)
            {
                reserved = attempt;
                status["resets"]!["next"]!["pending_attempt_id"] = attempt;
                throw new HttpRequestException("Response lost after reservation");
            }
            Assert.Equal(reserved, attempt);
            return new(HttpStatusCode.OK) { Content = new StringContent($$"""{"outcome":"reset","status":{{final.ToJsonString()}}}""") };
        });

        var endpoint = new Uri("https://agent.example");
        using (var http = new HttpClient(handler, disposeHandler: false))
        using (var original = new AgentClient(endpoint, new Vault(), http))
        {
            var offer = ReadOffer(await original.GetAsync(path));
            Assert.Null(offer.Credit.PendingAttemptId);
            await Assert.ThrowsAsync<HttpRequestException>(() => ResetCredits.ConsumeAsync(original, "p", "c", offer, default));
        }
        Assert.Equal(new[] { "GET", "POST" }, traffic);

        using var reopenedHttp = new HttpClient(handler, disposeHandler: false);
        using var reopened = new AgentClient(endpoint, new Vault(), reopenedHttp);
        var fresh = await reopened.GetAsync(path);
        Assert.Null(ResetCredits.Read(fresh, "other", "c"));
        Assert.Null(ResetCredits.Read(fresh, "p", "changed"));
        var pendingOffer = ReadOffer(fresh);
        Assert.Equal(reserved, pendingOffer.Credit.PendingAttemptId?.ToString("D"));
        Assert.Equal(new[] { "GET", "POST", "GET" }, traffic);

        var reply = await ResetCredits.ConsumeAsync(reopened, "p", "c", pendingOffer, default);
        Assert.Equal("reset", reply.Outcome);
        var remaining = Assert.IsType<ResetCredits>(ResetCredits.Read(reply.Status, "p", "c"));
        Assert.Equal(0, remaining.AvailableCount);
        Assert.Null(remaining.EligibleOffer(Now));
        Assert.Equal("used", Assert.Single(remaining.History).Kind);
        Assert.Equal(new[] { "GET", "POST", "GET", "POST" }, traffic);
    }

    [Theory]
    [InlineData(null)]
    [InlineData("not-a-uuid")]
    [InlineData("00000000-0000-0000-0000-000000000000")]
    [InlineData("ABCDEF12-1000-4000-9000-123456789abc")]
    public void InvalidPendingIdCannotBecomeAFreshAttempt(string? invalid)
    {
        var status = CompleteStatus();
        status["resets"]!["next"]!["pending_attempt_id"] = invalid;
        Assert.Null(ResetCredits.Read(status, "p", "c")?.EligibleOffer(Now));
    }

    private static JsonNode CompleteStatus() => JsonNode.Parse(Status.Replace("\"details_complete\":false", "\"details_complete\":true"))!;

    private static ResetOffer ReadOffer(JsonNode status) => Assert.IsType<ResetOffer>(ResetCredits.Read(status, "p", "c")?.EligibleOffer(Now));

    private sealed class Capture(Func<HttpRequestMessage, Task<HttpResponseMessage>> respond) : HttpMessageHandler
    {
        public int Calls { get; private set; }
        protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            Calls++;
            return respond(request);
        }
    }

    private sealed class Vault : ISessionVault
    {
        public Task<string?> ReadAsync(string endpoint) => Task.FromResult<string?>(null);
        public Task SaveAsync(string endpoint, string token) => Task.CompletedTask;
        public Task RemoveAsync(string endpoint) => Task.CompletedTask;
    }
}
