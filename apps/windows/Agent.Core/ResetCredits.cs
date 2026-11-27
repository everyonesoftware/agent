using System.Text.Json.Nodes;

namespace Agent.Core;

public sealed record ResetExpiration(int Count, long? ExpiresAt);
public sealed record ResetCredit(string Id, long? ExpiresAt, Guid? PendingAttemptId = null);
public sealed record ResetOffer(int AvailableCount, ResetCredit Credit);
public sealed record ResetEvent(string Kind, long At);
public sealed record ResetConsumption(string Outcome, JsonObject Status);

public sealed record ResetCredits(int AvailableCount, IReadOnlyList<ResetExpiration> Expirations,
    bool DetailsComplete, ResetCredit? Next, IReadOnlyList<ResetEvent> History, bool HistoryUnavailable)
{
    public ResetOffer? EligibleOffer(long now) => AvailableCount > 0 && DetailsComplete && !HistoryUnavailable &&
        Next is { } next && Expirations.Count > 0 && next.ExpiresAt == Expirations[0].ExpiresAt &&
        (next.ExpiresAt is null || next.ExpiresAt > now) ? new(AvailableCount, next) : null;

    public static ResetCredits? Read(JsonNode? status, string profile, string connection)
    {
        if (!MatchesScope(status, profile, connection) || status is not JsonObject result ||
            Value<string>(result["state"]) != "connected" ||
            Scalar<bool>(result["available"]) != true || !string.IsNullOrWhiteSpace(Value<string>(result["error"])) ||
            (result["snapshot"] is JsonObject snapshot && Scalar<bool>(snapshot["stale"]) == true) ||
            result["resets"] is not JsonObject resets || Scalar<int>(resets["available_count"]) is not { } count || count < 0 ||
            Scalar<bool>(resets["details_complete"]) is not { } complete ||
            resets["expirations"] is not JsonArray groups || resets["history"] is not JsonArray events)
            return null;

        var expirations = new List<ResetExpiration>();
        long groupedCount = 0;
        foreach (var entry in groups)
        {
            if (entry is not JsonObject group || Scalar<int>(group["count"]) is not { } quantity || quantity <= 0 ||
                !group.ContainsKey("expires_at") || !TryTime(group["expires_at"], out long? expires)) return null;
            groupedCount += quantity;
            if (groupedCount > count) return null;
            expirations.Add(new(quantity, expires));
        }
        complete &= groupedCount == count;

        ResetCredit? next = null;
        if (resets["next"] is { } nextNode)
        {
            if (count == 0 || nextNode is not JsonObject credit ||
                Value<string>(credit["credit_id"]) is not { } id || string.IsNullOrWhiteSpace(id) ||
                !credit.ContainsKey("expires_at") || !TryTime(credit["expires_at"], out long? expires)) return null;
            Guid? pending = null;
            if (credit.ContainsKey("pending_attempt_id"))
            {
                var wireId = Value<string>(credit["pending_attempt_id"]);
                if (!Guid.TryParseExact(wireId, "D", out var attempt) || attempt == Guid.Empty ||
                    attempt.ToString("D") != wireId) return null;
                pending = attempt;
            }
            next = new(id, expires, pending);
        }

        var history = new List<ResetEvent>();
        foreach (var entry in events)
        {
            if (entry is not JsonObject row || Value<string>(row["kind"]) is not ("used" or "expired") ||
                !TryTime(row["at"], out long? at) || at is null) return null;
            history.Add(new(Value<string>(row["kind"])!, at.Value));
        }
        return new(count, expirations.OrderBy(x => x.ExpiresAt ?? long.MaxValue).ToArray(), complete, next,
            history.OrderByDescending(x => x.At).ToArray(), Scalar<bool>(resets["history_unavailable"]) == true);
    }

    public static async Task<ResetConsumption> ConsumeAsync(AgentClient client, string profile, string connection,
        ResetOffer offer, CancellationToken cancellation)
    {
        // A pending ID is supplied by fresh, scoped status from the server's durable ledger.
        var reply = await client.PostAsync("/api/usage/resets/consume", new
        {
            profile_id = profile, connection_id = connection, credit_id = offer.Credit.Id,
            attempt_id = offer.Credit.PendingAttemptId ?? Guid.NewGuid(),
            confirmed_count = offer.AvailableCount, confirmed_expires_at = offer.Credit.ExpiresAt
        }, cancellation);
        if (reply is JsonObject envelope && Value<string>(envelope["outcome"]) is
            ("reset" or "alreadyRedeemed" or "nothingToReset" or "noCredit") and { } outcome &&
            envelope["status"] is JsonObject status && MatchesScope(status, profile, connection) &&
            !string.IsNullOrWhiteSpace(Value<string>(status["state"])))
            return new(outcome, status);
        throw new AgentException("The server did not confirm the reset outcome.");
    }

    private static bool MatchesScope(JsonNode? status, string profile, string connection) =>
        !string.IsNullOrWhiteSpace(profile) && !string.IsNullOrWhiteSpace(connection) && status is JsonObject result &&
        Value<string>(result["profile_id"]) == profile && Value<string>(result["connection_id"]) == connection;

    private static T? Value<T>(JsonNode? node) where T : class => node is JsonValue value && value.TryGetValue<T>(out var result) ? result : null;
    private static T? Scalar<T>(JsonNode? node) where T : struct => node is JsonValue value && value.TryGetValue<T>(out var result) ? result : null;
    private static bool TryTime(JsonNode? node, out long? seconds)
    {
        seconds = Scalar<long>(node);
        return node is null || seconds is > 0 and <= 253402300799;
    }
}
