using System.Text.Json.Nodes;

namespace Agent.Core;

public sealed record UsageChartPoint(double At, double Remaining, long ResetAt = 0, long WindowMinutes = 0);

public sealed record UsageChartForecast(double Rate, double Seconds, double Consumed, double DepletedSeconds,
    int ResetCount, double Lookback, UsageChartPoint End);

public sealed class UsageHistoryChart
{
    private UsageHistoryChart() { }

    public double Start { get; private init; }
    public long Now { get; private init; }
    public long ResetAt { get; private init; }
    public double? Remaining { get; private init; }
    public long? RemainingCount { get; private init; }
    public long? ReadingAge { get; private init; }
    public bool Outdated { get; private init; }
    public double? DailyBudget { get; private init; }
    public UsageChartForecast? Forecast { get; private init; }
    public string ForecastReason { get; private init; } = "";
    public UsageChartForecast? HourlyForecast { get; private init; }
    public string HourlyReason { get; private init; } = "";
    public bool Ledger { get; private init; }
    public UsageChartPoint[][] Segments { get; private init; } = [];
    public UsageChartPoint[][] DisplaySegments { get; private init; } = [];
    public (UsageChartPoint From, UsageChartPoint To)[] GapConnectors { get; private init; } = [];
    public UsageChartPoint[] Milestones { get; private init; } = [];
    public long[] Resets { get; private init; } = [];

    private const int Day = 86_400;
    private const int DisplayGap = 3_600;
    private static readonly double[] Levels = [90, 75, 50, 25, 10];

    public static UsageHistoryChart Create(JsonNode data, string range, long now)
    {
        var input = data as JsonObject;
        var ledger = Field(input, "provider") is JsonValue provider && provider.TryGetValue<string>(out var id) && id == "brave";
        var period = Positive(input, "period_start");
        var tracked = Positive(input, "tracking_started_at");
        var gap = Positive(input, "gap_seconds") is > 0 and var suppliedGap ? suppliedGap : 180;
        var start = range switch
        {
            "period" => Math.Min(now, period > 0 ? period : tracked > 0 ? tracked : now),
            "week" => now - 7 * Day,
            _ => now - Day
        };
        var points = ReadPoints(Field(input, "points"), now);
        var groups = Group(points, ledger, gap);
        var segments = Clip(groups, start);
        var display = Clip(Group(points, ledger, DisplayGap - 1), start);
        var connectors = new List<(UsageChartPoint From, UsageChartPoint To)>();
        for (int i = 1; !ledger && i < display.Length; i++)
        {
            var from = display[i - 1][^1];
            var to = display[i][0];
            if (to.At - from.At >= DisplayGap && Compatible(from, to)) connectors.Add((from, to));
        }

        var milestones = new List<UsageChartPoint>();
        foreach (var segment in segments)
        for (int i = 1; i < segment.Length; i++)
        {
            var from = segment[i - 1];
            var to = segment[i];
            foreach (var level in Levels)
                if (from.Remaining > level && to.Remaining <= level)
                    milestones.Add(new(ledger ? to.At : from.At + (from.Remaining - level) / (from.Remaining - to.Remaining) * (to.At - from.At), level));
        }

        var remaining = Percent(input);
        var resetAt = Positive(input, "reset_at");
        var observed = Positive(input, "observed_at");
        var maxAge = ledger ? 90 : gap;
        var stale = Field(input, "stale") is JsonValue flag && flag.TryGetValue<bool>(out var isStale) && isStale;
        long? readingAge = observed > 0 ? Math.Max(0, now - observed) : null;
        var fresh = !stale && observed > 0 && observed - now <= 5 && readingAge <= maxAge;
        // Forecasts use the server's raw coverage, not range-dependent display buckets.
        var evidence = Field(input, "forecast_points") is JsonArray raw
            ? Group(ReadPoints(raw, now), ledger, gap) : groups;
        var daily = Project(evidence, now, observed, gap, resetAt, remaining, fresh, ledger, Day, acrossGroups: true);
        var hourly = Project(evidence, now, observed, gap, resetAt, remaining, fresh, ledger, 3_600, acrossGroups: false);

        return new()
        {
            Start = start,
            Now = now,
            ResetAt = resetAt,
            Remaining = remaining,
            ReadingAge = readingAge,
            Outdated = stale || readingAge > maxAge,
            DailyBudget = fresh && resetAt > now ? remaining * Day / (resetAt - now) : null,
            Forecast = daily.Forecast,
            ForecastReason = daily.Reason,
            HourlyForecast = hourly.Forecast,
            HourlyReason = hourly.Reason,
            RemainingCount = Integer(input, "remaining_count"),
            Ledger = ledger,
            Segments = segments,
            DisplaySegments = display,
            GapConnectors = connectors.ToArray(),
            Milestones = milestones.ToArray(),
            Resets = points.Where(p => p.ResetAt > 0 && p.ResetAt >= p.At && p.ResetAt >= start && p.ResetAt <= now)
                .Select(p => p.ResetAt).Distinct().Order().ToArray()
        };
    }

    private static (UsageChartForecast? Forecast, string Reason) Project(UsageChartPoint[][] groups, long now,
        long observed, long gap, long resetAt, double? remaining, bool fresh, bool ledger, int window, bool acrossGroups)
    {
        if (!fresh || groups.Length == 0 || (!ledger && now - groups[^1][^1].At > gap))
            return (null, "Latest allowance reading is out of date.");
        var last = groups[^1][^1];
        if (remaining is not { } balance || resetAt <= now || (last.ResetAt > 0 && last.ResetAt != resetAt) || last.Remaining != balance)
            return (null, "Current allowance or reset time is unavailable or has changed.");

        // Ledger cutoff balances are anchored to the server snapshot, not receipt time.
        var measuredAt = ledger ? observed : last.At;
        var cutoff = measuredAt - window;
        var firstComparableAt = measuredAt;
        double seconds = 0, consumed = 0, depleted = 0;
        int resetCount = 0;
        long? previousReset = null;
        for (int index = acrossGroups ? 0 : groups.Length - 1; index < groups.Length; index++)
        {
            var group = groups[index];
            var tail = group[^1];
            if (last.WindowMinutes > 0 && tail.WindowMinutes > 0 && last.WindowMinutes != tail.WindowMinutes) continue;
            if (tail.At >= cutoff) firstComparableAt = Math.Min(firstComparableAt, group[0].At);
            var before = seconds;
            for (int i = 1; i < group.Length; i++)
            {
                var from = group[i - 1];
                var to = group[i];
                var duration = Math.Min(measuredAt, to.At) - Math.Max(cutoff, from.At);
                if (duration <= 0) continue;
                seconds += duration;
                // For the ledger the decline belongs to its event, not to the idle interval.
                consumed += (from.Remaining - to.Remaining) * (ledger ? 1 : duration / (to.At - from.At));
                if (from.Remaining == 0 && to.Remaining == 0) depleted += duration;
            }
            if (seconds <= before) continue;
            if (acrossGroups && previousReset.HasValue && previousReset != group[0].ResetAt) resetCount++;
            previousReset = tail.ResetAt;
        }
        if (ledger && last.At < measuredAt)
        {
            var duration = measuredAt - Math.Max(cutoff, last.At);
            firstComparableAt = Math.Min(firstComparableAt, last.At);
            seconds += duration;
            if (last.Remaining == 0) depleted += duration;
        }
        if (seconds <= 0) return (null, "A second timestamped allowance reading is needed to measure a pace.");
        if (!acrossGroups && seconds < window) return (null, "Needs a complete hour of recent history.");

        var rate = consumed / seconds;
        var exhaustion = balance == 0 ? now : rate > 0 ? now + balance / rate : double.PositiveInfinity;
        var end = new UsageChartPoint(Math.Min(resetAt, exhaustion), exhaustion <= resetAt ? 0 : Math.Max(0, balance - rate * (resetAt - now)));
        return (new(rate, seconds, consumed, depleted, resetCount, Math.Min(window, measuredAt - firstComparableAt), end), "");
    }

    // Input is one sorted, normalized display segment. These knots are for rendering only.
    public static UsageChartPoint[] Trend(UsageChartPoint[] segment, bool ledger)
    {
        if (segment.Length < 2) return segment.ToArray();
        var first = segment[0];
        var last = segment[^1];
        var window = Math.Max(900, (last.At - first.At) / 80);
        var step = window / 3;
        var area = new double[segment.Length];
        for (int i = 1; i < segment.Length; i++)
        {
            var from = segment[i - 1];
            var to = segment[i];
            area[i] = area[i - 1] + (to.At - from.At) * (ledger ? from.Remaining : (from.Remaining + to.Remaining) / 2);
        }

        double Integral(double at)
        {
            if (at <= first.At) return (at - first.At) * first.Remaining;
            if (at >= last.At) return area[^1] + (at - last.At) * last.Remaining;
            int lo = 0, hi = segment.Length - 1;
            while (hi - lo > 1)
            {
                var mid = lo + (hi - lo) / 2;
                if (segment[mid].At <= at) lo = mid;
                else hi = mid;
            }
            var from = segment[lo];
            var to = segment[hi];
            var elapsed = at - from.At;
            return area[lo] + from.Remaining * elapsed + (ledger ? 0 :
                (to.Remaining - from.Remaining) * elapsed * elapsed / (2 * (to.At - from.At)));
        }

        var trend = new List<UsageChartPoint> { first };
        for (int i = 1; i <= 240; i++)
        {
            var at = first.At + step * i;
            if (at >= last.At) break;
            var mean = (Integral(at + window / 2) - Integral(at - window / 2)) / window;
            trend.Add(new(at, Math.Max(last.Remaining, Math.Min(trend[^1].Remaining, mean))));
        }
        trend.Add(last);
        return trend.ToArray();
    }

    private static JsonNode? Field(JsonObject? obj, string name) => obj?[name];
    private static long? Integer(JsonObject? obj, string name) => Field(obj, name) is JsonValue field
        ? field.TryGetValue<long>(out var integer) ? integer : field.TryGetValue<int>(out var small) ? small : null
        : null;
    private static long Positive(JsonObject? obj, string name) => Math.Max(0, Integer(obj, name) ?? 0);
    private static double? Percent(JsonObject? obj)
    {
        if (Field(obj, "remaining_percent") is not JsonValue field) return null;
        double? percent = field.TryGetValue<double>(out var number) ? number : Integer(obj, "remaining_percent");
        return percent is >= 0 and <= 100 ? percent : null;
    }

    private static UsageChartPoint[] ReadPoints(JsonNode? node, long now)
    {
        if (node is not JsonArray array) return [];
        var byTime = new SortedDictionary<long, UsageChartPoint>();
        foreach (var obj in array.OfType<JsonObject>())
        {
            var at = Positive(obj, "at");
            if (at > 0 && at <= now && Percent(obj) is { } remaining)
                byTime[at] = new(at, remaining, Positive(obj, "reset_at"), Positive(obj, "window_minutes"));
        }
        return byTime.Values.ToArray();
    }

    private static bool Compatible(UsageChartPoint from, UsageChartPoint to) =>
        (from.ResetAt == 0 || (to.At < from.ResetAt && (to.ResetAt == 0 || to.ResetAt == from.ResetAt))) &&
        (from.WindowMinutes == 0 || to.WindowMinutes == 0 || from.WindowMinutes == to.WindowMinutes) &&
        to.Remaining <= from.Remaining;

    private static UsageChartPoint[][] Group(UsageChartPoint[] points, bool ledger, long gap)
    {
        var groups = new List<UsageChartPoint[]>();
        var start = 0;
        for (int i = 1; i < points.Length; i++)
        {
            var from = points[i - 1];
            var to = points[i];
            if ((ledger || to.At - from.At <= gap) && Compatible(from, to)) continue;
            groups.Add(points[start..i]);
            start = i;
        }
        if (start < points.Length) groups.Add(points[start..]);
        return groups.ToArray();
    }

    private static UsageChartPoint[][] Clip(UsageChartPoint[][] groups, long start) => groups
        .Select(group => group.Where(point => point.At >= start).ToArray()).Where(group => group.Length > 0).ToArray();
}
