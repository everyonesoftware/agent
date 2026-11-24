using System.Text.Json.Nodes;
using Agent.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Automation.Peers;
using Microsoft.UI.Xaml.Controls;

namespace Agent.App;

public sealed partial class MainWindow
{
    private async Task ShowUsageHistoryAsync(bool research)
    {
        var c = Need();
        long epoch = accountEpoch;
        string profile = activeProfile;
        if (!research && profile == "") throw new AgentException("Select an active ChatGPT account first.");
        string title = research ? "Brave Search usage" : "ChatGPT usage";

        var range = new ComboBox { MinHeight = 36, VerticalAlignment = VerticalAlignment.Center };
        AutomationProperties.SetName(range, "History range");
        foreach (var (id, label) in new[] { ("24h", "24 hours"), ("week", "1 week"), ("period", "Entire period") })
            range.Items.Add(new ComboBoxItem { Content = label, Tag = id });
        range.SelectedIndex = 0;
        var close = new Button { Content = "×", FontSize = 18, MinHeight = 36, MinWidth = 36, Padding = new Thickness(0) };
        AutomationProperties.SetName(close, "Close usage history");
        var heading = UsageHistoryPlot.Label(title, 18, UsageHistoryPlot.Ink);
        heading.FontWeight = Microsoft.UI.Text.FontWeights.SemiBold;
        heading.VerticalAlignment = VerticalAlignment.Center;
        AutomationProperties.SetHeadingLevel(heading, AutomationHeadingLevel.Level1);
        var header = HistoryRow(heading, range, close);
        header.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        header.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });
        var windows = new ComboBox { Header = "Allowance window", Visibility = Visibility.Collapsed, HorizontalAlignment = HorizontalAlignment.Stretch };
        var display = new StackPanel { Spacing = 12 };
        var detail = new StackPanel { Spacing = 8 };
        var detailContent = new StackPanel { Spacing = 10, Children = { windows, detail } };
        var info = new Expander { Header = "Forecast details", Content = detailContent, HorizontalAlignment = HorizontalAlignment.Stretch, HorizontalContentAlignment = HorizontalAlignment.Stretch, Visibility = Visibility.Collapsed };
        var body = new StackPanel { Spacing = 10, Padding = new Thickness(0, 0, 8, 0), Children = { display, info } };
        var scroller = new ScrollViewer { Content = body, HorizontalScrollMode = ScrollMode.Disabled, HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled };
        var content = new StackPanel { Spacing = 16, Children = { header, scroller } };
        var dialog = new ContentDialog
        {
            XamlRoot = root.XamlRoot, RequestedTheme = ElementTheme.Dark, Background = UsageHistoryPlot.Panel,
            BorderBrush = UsageHistoryPlot.Rule, BorderThickness = new Thickness(1), CornerRadius = new CornerRadius(10), Content = content
        };
        AutomationProperties.SetName(dialog, title);
        dialog.Resources["ContentDialogMaxWidth"] = 944d;
        dialog.Resources["ContentDialogMinWidth"] = 0d;
        void Resize()
        {
            content.Width = Math.Clamp(root.ActualWidth - 104, 0, 896);
            bool compact = content.Width < 390;
            Grid.SetRow(range, compact ? 1 : 0);
            Grid.SetColumn(range, compact ? 0 : 1);
            scroller.MaxHeight = Math.Clamp(root.ActualHeight - (compact ? 194 : 146), 0, 660);
        }
        Resize();
        SizeChangedEventHandler resize = (_, _) => Resize();
        close.Click += (_, _) => dialog.Hide();
        dialog.Opened += (_, _) => range.Focus(FocusState.Programmatic);
        content.KeyDown += (_, e) =>
        {
            if (e.Handled || e.Key != Windows.System.VirtualKey.Escape) return;
            e.Handled = true;
            if (info.IsExpanded)
            {
                info.IsExpanded = false;
                info.Focus(FocusState.Keyboard);
            }
            else dialog.Hide();
        };

        using var lifetime = CancellationTokenSource.CreateLinkedTokenSource(accountLifetime.Token);
        CancellationTokenSource? pending = null;
        long revision = 0;
        bool filling = false, open = true;
        string selectedWindow = "";
        JsonNode? lastResult = null;
        var timer = DispatcherQueue.CreateTimer();
        timer.Interval = TimeSpan.FromMinutes(1);
        bool OwnsDialog() => open && IsCurrentSession(c, epoch) && (research || activeProfile == profile);
        async Task Read(bool refresh = false)
        {
            if (!OwnsDialog()) return;
            long requestRevision = ++revision;
            pending?.Cancel();
            using var request = CancellationTokenSource.CreateLinkedTokenSource(lifetime.Token);
            pending = request;
            bool OwnsRead() => requestRevision == revision && OwnsDialog();
            if (!refresh)
            {
                lastResult = null;
                display.Children.Clear();
                display.Children.Add(UsageHistoryPlot.Label("Loading usage history…", 14));
                detail.Children.Clear();
            }
            string rangeId = (range.SelectedItem as ComboBoxItem)?.Tag as string ?? "24h";
            try
            {
                string query = "range=" + Wire.Segment(rangeId);
                if (!research)
                    query += "&profile_id=" + Wire.Segment(profile) + (selectedWindow == "" ? "" : "&window_id=" + Wire.Segment(selectedWindow));
                var result = await c.GetAsync((research ? "/api/research/history?" : "/api/usage/history?") + query, request.Token);
                if (!OwnsRead()) return;
                var model = UsageHistoryChart.Create(result, rangeId, DateTimeOffset.UtcNow.ToUnixTimeSeconds());
                if (!research)
                {
                    filling = true;
                    try
                    {
                        selectedWindow = result.Text("window_id");
                        var options = result.Items("windows").Select(item => (Id: item.Text("id"), Label: item.Text("label"))).ToArray();
                        if (!windows.Items.Cast<ComboBoxItem>().Select(item => ((string)item.Tag, (string)item.Content)).SequenceEqual(options))
                        {
                            windows.Items.Clear();
                            foreach (var option in options) windows.Items.Add(new ComboBoxItem { Content = option.Label, Tag = option.Id });
                        }
                        windows.SelectedIndex = Array.FindIndex(options, item => item.Id == selectedWindow);
                        windows.Visibility = windows.Items.Count > 1 ? Visibility.Visible : Visibility.Collapsed;
                    }
                    finally { filling = false; }
                }
                RenderHistory(display, detail, model, result, rangeId);
                lastResult = result;
                info.Visibility = Visibility.Visible;
            }
            catch (OperationCanceledException) when (request.IsCancellationRequested) { }
            catch (SessionExpiredException) { if (OwnsRead()) ClearAccount(); }
            catch
            {
                if (OwnsRead())
                {
                    if (refresh && lastResult?.DeepClone() is JsonObject cached)
                    {
                        cached["stale"] = true;
                        cached["warning"] = "Usage history could not be refreshed. Showing last-known observations.";
                        RenderHistory(display, detail, UsageHistoryChart.Create(cached, rangeId, DateTimeOffset.UtcNow.ToUnixTimeSeconds()), cached, rangeId);
                    }
                    else
                    {
                        display.Children.Clear();
                        display.Children.Add(UsageHistoryPlot.Label("Usage history could not be loaded. Change range or reopen to retry.", 14, UsageHistoryPlot.Amber));
                    }
                }
            }
            finally { if (ReferenceEquals(pending, request)) pending = null; }
        }

        range.SelectionChanged += async (_, _) => await Read();
        windows.SelectionChanged += async (_, _) =>
        {
            if (filling || !OwnsDialog() || windows.SelectedItem is not ComboBoxItem { Tag: string id }) return;
            selectedWindow = id;
            await Read();
        };
        timer.Tick += async (_, _) => { if (pending is null) await Read(refresh: true); };
        dialog.Opened += async (_, _) =>
        {
            timer.Start();
            await Read();
        };
        currentDialog = dialog;
        root.SizeChanged += resize;
        try { await dialog.ShowAsync(); }
        finally
        {
            open = false;
            timer.Stop();
            lifetime.Cancel();
            root.SizeChanged -= resize;
            if (currentDialog == dialog) currentDialog = null;
        }
    }

    private static void RenderHistory(StackPanel display, StackPanel detail, UsageHistoryChart model, JsonNode result, string range)
    {
        var oldPlot = display.Children.OfType<UsageHistoryPlot>().FirstOrDefault();
        var plotFocus = oldPlot?.FocusState ?? FocusState.Unfocused;
        display.Children.Clear();
        detail.Children.Clear();
        string current = model.Ledger && model.RemainingCount is long count ? $"{count:N0} searches left" :
            model.Remaining is double remaining ? $"{remaining:0.#}% left" : "Unavailable";
        var value = UsageHistoryPlot.Label(current, 26, UsageHistoryPlot.Ink);
        value.FontWeight = Microsoft.UI.Text.FontWeights.SemiBold;
        string reset = model.ResetAt > model.Now ? "Resets in " + HistoryDuration(model.ResetAt - model.Now) : "Reset time unavailable";
        if (model.Segments.Length > 0 && model.ResetAt > UsageHistoryPlot.EndAt(model, range)) reset += " · beyond chart";
        var resetText = UsageHistoryPlot.Label(reset, 12, UsageHistoryPlot.Amber);
        resetText.MaxWidth = 145;
        resetText.VerticalAlignment = VerticalAlignment.Center;
        display.Children.Add(HistoryRow(value, resetText));

        var forecast = model.Forecast;
        bool overBudget = forecast is not null && forecast.End.At < model.ResetAt;
        bool partial = forecast is not null && forecast.Lookback - forecast.Seconds > 1;
        bool shortHistory = forecast is not null && forecast.Lookback < 86400;
        var color = overBudget ? UsageHistoryPlot.Amber : UsageHistoryPlot.Accent;
        string estimate = forecast is null ? "Forecast unavailable" : model.Remaining == 0 ? "Allowance depleted" :
            forecast.Rate == 0 ? "No usage recorded in the observed history" : overBudget ?
            "May run out in about " + HistoryDuration(forecast.End.At - model.Now) : $"About {forecast.End.Remaining:0.#}% projected at reset";
        display.Children.Add(UsageHistoryPlot.Label(estimate, 15, forecast is null ? null : color));
        var metrics = new List<string> { model.DailyBudget is double budget ? $"Daily budget: {budget:0.#}% / day" : "Daily budget unavailable" };
        if (forecast is not null)
        {
            string pace = shortHistory ? "Short-term pace" : partial ? "Covered-history pace" : "Past 24 hours";
            metrics.Add($"{pace}: {forecast.Rate * 86400:0.#}% / day");
            metrics.Add((overBudget ? "Above budget" : "Within budget") + (partial ? " · partial coverage" : ""));
        }
        display.Children.Add(UsageHistoryPlot.Label(string.Join("    ", metrics), 12));
        string warning = result.Text("warning");
        if (warning == "" && model.Outdated)
            warning = model.ReadingAge is long age ? $"Allowance last checked {HistoryDuration(age)} ago; current allowance may have changed." : "Last-known observations; current allowance may have changed.";
        if (warning == "" && model.ReadingAge is > 60)
            warning = $"Allowance last checked {HistoryDuration(model.ReadingAge.Value)} ago; updates are delayed.";
        if (warning != "") display.Children.Add(UsageHistoryPlot.Label(warning, 12, UsageHistoryPlot.Amber));

        bool hasPlot = model.Segments.Length > 0 && (!model.Ledger || model.Remaining.HasValue);
        if (hasPlot)
        {
            var plot = new UsageHistoryPlot(model, range);
            if (oldPlot is not null) plot.RestoreInspection(oldPlot);
            display.Children.Add(plot);
            if (plotFocus != FocusState.Unfocused) plot.Focus(plotFocus);
        }
        else display.Children.Add(UsageHistoryPlot.Label(model.Ledger && !model.Remaining.HasValue
            ? "No remaining allowance percentage is available for this search cap."
            : "No recorded history for this window yet. Missing data does not mean no allowance remains.", 13));

        string basis = forecast is null ? model.ForecastReason :
            "Forecast: " + (shortHistory ? "recent " + HistoryDuration(forecast.Lookback) : "past 24 hours") +
            " · " + HistoryDuration(forecast.Seconds) + " covered" + (forecast.ResetCount > 0 ? " · spans a reset" : "");
        display.Children.Add(new Border { BorderBrush = UsageHistoryPlot.Rule, BorderThickness = new Thickness(0, 1, 0, 0) });
        display.Children.Add(UsageHistoryPlot.Label(hasPlot ? basis + "    ━ Recorded" + (forecast is not null ? "    ╌ Recent average" : "") : basis, 12));
        string scope = model.Ledger ? "Brave Search · Agent cap, not provider billing" : "ChatGPT · " +
            (result.Items("windows").FirstOrDefault(w => w.Text("id") == result.Text("window_id"))?.Text("label") ?? "Codex-reported allowance");
        detail.Children.Add(UsageHistoryPlot.Label(scope, 13, UsageHistoryPlot.Ink));
        if (forecast is not null)
        {
            detail.Children.Add(UsageHistoryPlot.Label($"{forecast.Consumed:0.#}% consumed, including observed idle time. Pace: {forecast.Rate * 86400:0.#}% / day.", 12));
            if (forecast.DepletedSeconds > 0)
                detail.Children.Add(UsageHistoryPlot.Label($"Depleted for {HistoryDuration(forecast.DepletedSeconds)}; consumption excludes demand blocked by the limit.", 12));
        }
        string hourly = model.HourlyForecast is not { } recent ? model.HourlyReason : recent.Rate == 0 ?
            "No usage recorded." : $"{recent.Rate * 3600:0.#}% used. If that pace continues: " +
            (recent.End.At == model.ResetAt ? "resets before running out." : $"about {HistoryDuration(recent.End.At - model.Now)} left.");
        detail.Children.Add(UsageHistoryPlot.Label("Last hour: " + hourly, 12));
        detail.Children.Add(UsageHistoryPlot.Label("Recorded line is a time-smoothed trend. Inspection shows exact readings and identifies threshold crossings and projections. " +
            (model.Ledger ? "" : "Dotted gaps are not readings or forecast evidence. ") + "Forecasts are estimates.", 12));
    }

    private static Grid HistoryRow(params FrameworkElement[] elements)
    {
        var row = new Grid { ColumnSpacing = 10 };
        for (int i = 0; i < elements.Length; ++i)
        {
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = i == 0 ? new GridLength(1, GridUnitType.Star) : GridLength.Auto });
            Grid.SetColumn(elements[i], i);
            row.Children.Add(elements[i]);
        }
        return row;
    }

    private static string HistoryDuration(double seconds) => seconds switch
    {
        < 60 => $"{seconds:0} sec",
        < 3600 => $"{seconds / 60:0} min",
        < 86400 => $"{seconds / 3600:0.#} hour{(Math.Round(seconds / 3600, 1) == 1 ? "" : "s")}",
        _ => $"{seconds / 86400:0.#} day{(Math.Round(seconds / 86400, 1) == 1 ? "" : "s")}"
    };
}
