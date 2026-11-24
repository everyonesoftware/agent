using System.Text.Json.Nodes;
using Xunit;

namespace Agent.Core.Tests;

public sealed class UsageHistoryChartTests
{
    private const long Now = 2_000_000;

    [Fact]
    public void SampledHistorySeparatesCoverageFromDisplayAndDoesNotBridgeResetsOrIncreases()
    {
        var chart = Create("""
            {"provider":"chatgpt","gap_seconds":180,"points":[
              {"at":1990300,"remaining_percent":80,"reset_at":1995000},
              {"at":1990000,"remaining_percent":90,"reset_at":1995000},
              {"at":1990300,"remaining_percent":78,"reset_at":1995000},
              {"at":1993900,"remaining_percent":72,"reset_at":1995000},
              {"at":1995000,"remaining_percent":71,"reset_at":2009000},
              {"at":1998600,"remaining_percent":85,"reset_at":2009000},
              {"at":1998780,"remaining_percent":70,"reset_at":2009000},
              {"at":1998840,"remaining_percent":69,"reset_at":2009100},
              {"at":1999200,"remaining_percent":68,"reset_at":2009100,"window_minutes":60},
              {"at":1999260,"remaining_percent":67,"reset_at":2009100,"window_minutes":120},
              {"at":2000001,"remaining_percent":50},
              {"at":1999999,"remaining_percent":101},
              {"at":1999998,"remaining_percent":"60"},
              {"at":1999997,"remaining_percent":null},
              null
            ]}
            """);

        Assert.Equal(new[] { 1, 1, 1, 1, 2, 1, 1, 1 }, chart.Segments.Select(s => s.Length));
        Assert.Equal(new[] { 2, 1, 1, 2, 2, 1 }, chart.DisplaySegments.Select(s => s.Length));
        var connector = Assert.Single(chart.GapConnectors);
        Assert.Equal((1990300d, 78d, 1993900d), (connector.From.At, connector.From.Remaining, connector.To.At));
        Assert.Equal(new[] { 1995000L }, chart.Resets);
        var milestone = Assert.Single(chart.Milestones);
        Assert.Equal((1998720d, 75d), (milestone.At, milestone.Remaining));
    }

    [Theory]
    [InlineData("24h", 1913600)]
    [InlineData("week", 1395200)]
    [InlineData("period", 1990000)]
    public void RangeClipsHistoryWithoutInventingAPointAtItsStart(string range, long start)
    {
        var chart = Create("""
            {"provider":"chatgpt","period_start":1990000,"points":[
              {"at":1395199,"remaining_percent":94},
              {"at":1395201,"remaining_percent":93},
              {"at":1913601,"remaining_percent":92},
              {"at":1990001,"remaining_percent":91}
            ]}
            """, range);

        Assert.Equal(start, chart.Start);
        Assert.Equal(start + 1, chart.Segments[0][0].At);
    }

    [Theory]
    [InlineData("chatgpt")]
    [InlineData("brave")]
    public void RecordedHistoryEndsAtOrBeforeNowAndCanLagWhenRefreshFails(string provider)
    {
        var snapshot = JsonNode.Parse($$"""
            {"provider":"{{provider}}","points":[
              {"at":1999940,"remaining_percent":95},
              {"at":2000000,"remaining_percent":72}
            ]}
            """)!.AsObject();
        var result = snapshot.DeepClone();
        result["points"]!.AsArray().Add(new JsonObject { ["at"] = Now + 1, ["remaining_percent"] = 60 });
        var fresh = UsageHistoryChart.Create(result, "24h", Now);
        snapshot["stale"] = true;
        var stale = UsageHistoryChart.Create(snapshot, "24h", Now + 60);

        foreach (var chart in new[] { fresh, stale })
        {
            Assert.Equal(Now, chart.Segments.SelectMany(segment => segment).Max(point => point.At));
            Assert.Equal(Now, chart.DisplaySegments.SelectMany(segment => UsageHistoryChart.Trend(segment, chart.Ledger)).Max(point => point.At));
            Assert.NotEmpty(chart.Milestones);
            Assert.All(chart.Milestones, point => Assert.True(point.At <= chart.Now));
        }
        Assert.Equal(Now, fresh.Now);
        Assert.Equal(Now + 60, stale.Now);
        Assert.True(stale.Outdated);
        Assert.Null(stale.Forecast);
    }

    [Fact]
    public void LedgerHoldsContinuityAcrossLongIdleIntervalsAndPlacesThresholdsAtEvents()
    {
        var chart = Create("""
            {"provider":"brave","observed_at":2000000,"remaining_percent":70,
             "remaining_count":700,"reset_at":2010000,"points":[
              {"at":1990000,"remaining_percent":95,"reset_at":2010000},
              {"at":1998000,"remaining_percent":70,"reset_at":2010000}
            ]}
            """);

        Assert.True(chart.Ledger);
        Assert.Equal(700L, chart.RemainingCount);
        Assert.Equal(2010000, chart.ResetAt);
        Assert.Equal(70d, chart.Remaining);
        Assert.Equal(2, Assert.Single(chart.Segments).Length);
        Assert.Equal(2, Assert.Single(chart.DisplaySegments).Length);
        Assert.Empty(chart.GapConnectors);
        Assert.Collection(chart.Milestones,
            point => Assert.Equal((1998000d, 90d), (point.At, point.Remaining)),
            point => Assert.Equal((1998000d, 75d), (point.At, point.Remaining)));
    }

    [Fact]
    public void ProjectionUsesCompatibleRawCoverageAcrossResetsButNotGapsOrDelayedWallTime()
    {
        const string data = """
            {"provider":"chatgpt","observed_at":1999900,"remaining_percent":60,"reset_at":2001000,
             "points":[{"at":1999900,"remaining_percent":60}],"forecast_points":[
              {"at":1999100,"remaining_percent":80,"window_minutes":60,"reset_at":1999400},
              {"at":1999200,"remaining_percent":70,"window_minutes":60,"reset_at":1999400},
              {"at":1999500,"remaining_percent":30,"window_minutes":300,"reset_at":1999700},
              {"at":1999600,"remaining_percent":20,"window_minutes":300,"reset_at":1999700},
              {"at":1999800,"remaining_percent":70,"window_minutes":300,"reset_at":2001000},
              {"at":1999900,"remaining_percent":60,"window_minutes":300,"reset_at":2001000}
            ]}
            """;
        var chart = Create(data);
        var forecast = Assert.IsType<UsageChartForecast>(chart.Forecast);

        Assert.Equal(forecast, Create(data, "week").Forecast);
        Assert.Equal(200, forecast.Seconds);
        Assert.Equal(400, forecast.Lookback);
        Assert.Equal(20, forecast.Consumed);
        Assert.Equal(1, forecast.ResetCount);
        Assert.Equal(0.1, forecast.Rate);
        Assert.Equal(new UsageChartPoint(Now + 600, 0), forecast.End);
        Assert.Empty(chart.Milestones);
        Assert.Null(chart.HourlyForecast);
        Assert.Equal(5184, chart.DailyBudget);
        Assert.Equal(100, chart.ReadingAge);
        Assert.False(chart.Outdated);
    }

    [Theory]
    [InlineData(180, false, true)]
    [InlineData(181, false, false)]
    [InlineData(0, true, false)]
    public void FreshnessIsInclusiveAndStaleHistoryRemainsInspectable(long age, bool stale, bool fresh)
    {
        var chart = Create($$"""
            {"provider":"chatgpt","observed_at":{{Now-age}},"stale":{{(stale ? "true" : "false")}},
             "reset_at":2001000,"remaining_percent":60,"points":[
              {"at":{{Now-age-100}},"remaining_percent":70,"reset_at":2001000},
              {"at":{{Now-age}},"remaining_percent":60,"reset_at":2001000}
            ]}
            """);

        Assert.Equal(age, chart.ReadingAge);
        Assert.Equal(!fresh, chart.Outdated);
        Assert.Equal(fresh, chart.DailyBudget.HasValue);
        Assert.Equal(2, Assert.Single(chart.Segments).Length);
        Assert.Equal(fresh, chart.Forecast is not null);
        Assert.Equal(fresh ? "" : "Latest allowance reading is out of date.", chart.ForecastReason);
    }

    [Fact]
    public void LedgerUsesExactCutoffBalancesAndCountsObservedIdleThroughNow()
    {
        var chart = Create("""
            {"provider":"brave","observed_at":2000000,"reset_at":2100000,"remaining_percent":10,
             "points":[{"at":1990000,"remaining_percent":100},{"at":2000000,"remaining_percent":10}],
             "forecast_points":[{"at":1990000,"remaining_percent":30},{"at":1996400,"remaining_percent":10}]}
            """);

        var daily = Assert.IsType<UsageChartForecast>(chart.Forecast);
        Assert.Equal(10_000, daily.Seconds);
        Assert.Equal(20, daily.Consumed);
        Assert.Equal(new UsageChartPoint(Now + 5000, 0), daily.End);
        var hourly = Assert.IsType<UsageChartForecast>(chart.HourlyForecast);
        Assert.Equal(3_600, hourly.Seconds);
        Assert.Equal(0, hourly.Consumed);
        Assert.Equal(new UsageChartPoint(chart.ResetAt, 10), hourly.End);
    }

    [Fact]
    public void LedgerHourIsMeasuredAtTheSnapshotEvenWhenTheResponseArrivesLate()
    {
        var chart = UsageHistoryChart.Create(JsonNode.Parse("""
            {"provider":"brave","observed_at":2000000,"reset_at":2100000,"remaining_percent":90,
             "forecast_points":[{"at":1990000,"remaining_percent":100},{"at":1996400,"remaining_percent":100},
                               {"at":1996401,"remaining_percent":90},{"at":2000000,"remaining_percent":90}]}
            """)!, "24h", Now + 2);
        Assert.Equal(10_000, Assert.IsType<UsageChartForecast>(chart.Forecast).Seconds);
        Assert.Equal(10, Assert.IsType<UsageChartForecast>(chart.HourlyForecast).Consumed);
    }

    [Fact]
    public void FreshReadingWithoutCompatibleEvidenceOrFutureResetDoesNotProject()
    {
        const string data = """
            {"provider":"chatgpt","observed_at":2000000,"reset_at":2001000,"remaining_percent":60,
             "points":[{"at":1999900,"remaining_percent":70},{"at":2000000,"remaining_percent":60}],
             "forecast_points":[]}
            """;
        var chart = Create(data);
        Assert.Null(chart.Forecast);
        Assert.Equal(5184, chart.DailyBudget);

        chart = Create(data.Replace("[]", "[{\"at\":2000000,\"remaining_percent\":59}]"));
        Assert.Null(chart.Forecast);
        Assert.Contains("has changed", chart.ForecastReason);

        chart = Create(data.Replace("2001000", "2000000"));
        Assert.Null(chart.Forecast);
        Assert.Null(chart.DailyBudget);
    }

    [Fact]
    public void SampledTrendWeightsElapsedTimeAndAnchorsExactEndpoints()
    {
        UsageChartPoint[] sparse = [new(1000, 100), new(2800, 0)];
        UsageChartPoint[] uneven = [sparse[0], new(1180, 90), new(2260, 30), sparse[1]];
        var trend = UsageHistoryChart.Trend(uneven, ledger: false);

        Assert.Equal(sparse[0], trend[0]);
        Assert.Equal(sparse[^1], trend[^1]);
        Assert.Equal(7, trend.Length);
        Assert.Equal(50, trend[3].Remaining, precision: 8);
        Assert.Equal(82.63888889, trend[1].Remaining, precision: 8);
        Assert.Equal(UsageHistoryChart.Trend(sparse, ledger: false), trend);
        Assert.Equal(new[] { 100d, 90d, 30d, 0d }, uneven.Select(p => p.Remaining));
    }

    [Fact]
    public void LedgerTrendHoldsThePreEventBalanceDuringIdleTime()
    {
        UsageChartPoint[] events = [new(1000, 100), new(2200, 60), new(2800, 60)];
        var trend = UsageHistoryChart.Trend(events, ledger: true);

        Assert.Equal(100, trend.Single(p => p.At == 1600).Remaining);
        Assert.Equal(93.33333333, trend.Single(p => p.At == 1900).Remaining, precision: 8);
        Assert.Equal(events[^1], trend[^1]);
        Assert.All(trend.Zip(trend.Skip(1)), p => Assert.InRange(p.Second.Remaining, 60, p.First.Remaining));
    }

    [Fact]
    public void TrendKeepsSparseAndIdleSegmentsValidAndBoundsLongOutput()
    {
        var point = new UsageChartPoint(2000, 50);
        Assert.Empty(UsageHistoryChart.Trend([], ledger: false));
        Assert.Equal(point, Assert.Single(UsageHistoryChart.Trend([point], ledger: true)));
        var idle = UsageHistoryChart.Trend([point, new(point.At + 86_400 * 90, 50)], ledger: true);
        Assert.InRange(idle.Length, 200, 242);
        Assert.All(idle, p => Assert.Equal(50, p.Remaining));
    }

    private static UsageHistoryChart Create(string json, string range = "24h") =>
        UsageHistoryChart.Create(JsonNode.Parse(json)!, range, Now);
}
