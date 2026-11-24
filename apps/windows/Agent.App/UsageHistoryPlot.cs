using Agent.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Automation.Peers;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Input;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Shapes;
using Windows.Foundation;
using Windows.System;
using Path = Microsoft.UI.Xaml.Shapes.Path;

namespace Agent.App;

internal sealed class UsageHistoryPlot : UserControl
{
    internal static readonly SolidColorBrush Panel = Brush(36, 41, 38), Ink = Brush(225, 230, 226),
        Subtle = Brush(163, 174, 166), Rule = Brush(57, 65, 57), Accent = Brush(180, 214, 163), Amber = Brush(216, 189, 137);
    private const double Left = 40, Top = 42, HitRadius = 12;
    private readonly UsageHistoryChart model;
    private readonly string range;
    private readonly double end;
    private readonly (UsageChartPoint[] Raw, UsageChartPoint[] Trend)[] series;
    private readonly Canvas canvas = new() { Background = new SolidColorBrush(Microsoft.UI.Colors.Transparent) };
    private readonly TextBlock tipText = Label("", 12, Ink);
    private readonly Border tip;
    private readonly Line cursor = new() { Stroke = Subtle, StrokeThickness = 1, StrokeDashArray = new DoubleCollection { 2, 3 }, IsHitTestVisible = false };
    private readonly Ellipse dot = new() { Width = 10, Height = 10, Fill = Accent, Stroke = Panel, StrokeThickness = 2, IsHitTestVisible = false };
    private readonly List<(Point From, Point To, int Series)> hitLines = [];
    private readonly Reading[] options;
    private int index;
    private Reading? selected;
    private bool pinned;
    private double right, bottom;

    private enum Kind { Recorded, Threshold, Forecast }
    private sealed record Reading(UsageChartPoint Point, Kind Kind);

    internal static double EndAt(UsageHistoryChart chart, string range) => chart.Now + Math.Max(60,
        range == "period" && chart.ResetAt > chart.Now ? chart.ResetAt - chart.Now : Math.Max(60, chart.Now - chart.Start) * 2 / 3);

    internal UsageHistoryPlot(UsageHistoryChart model, string range)
    {
        this.model = model;
        this.range = range;
        end = EndAt(model, range);
        series = model.DisplaySegments.Select(g => (g, UsageHistoryChart.Trend(g, model.Ledger))).ToArray();
        IEnumerable<Reading> readings = model.Segments.SelectMany(group => group).Select(point => new Reading(point, Kind.Recorded))
            .Concat(model.Milestones.Select(point => new Reading(point, Kind.Threshold)));
        if (model.Forecast is { } forecast && model.Remaining is double remaining)
        {
            double at = Math.Min(end, forecast.End.At);
            readings = readings.Append(new Reading(new UsageChartPoint(at, Math.Max(0, remaining - forecast.Rate * (at - model.Now))), Kind.Forecast));
        }
        options = readings.OrderBy(p => p.Point.At).ToArray();
        index = Math.Max(0, Array.FindLastIndex(options, p => p.Kind == Kind.Recorded));
        tip = new Border
        {
            Child = tipText, Background = Panel, BorderBrush = Rule, BorderThickness = new Thickness(1),
            CornerRadius = new CornerRadius(6), Padding = new Thickness(8), IsHitTestVisible = false
        };
        AutomationProperties.SetLiveSetting(tipText, AutomationLiveSetting.Polite);
        AutomationProperties.SetName(this, "Remaining allowance over time");
        AutomationProperties.SetHelpText(this, "Solid line: recorded history. Dashed line: recent-average projection. Use Left and Right or Home and End to inspect readings, Enter to pin, Escape to clear.");
        AutomationProperties.SetAccessibilityView(cursor, AccessibilityView.Raw);
        AutomationProperties.SetAccessibilityView(dot, AccessibilityView.Raw);
        IsTabStop = true;
        UseSystemFocusVisuals = true;
        HorizontalContentAlignment = HorizontalAlignment.Stretch;
        Content = canvas;
        Height = 300;
        SizeChanged += (_, _) => Draw();
        PointerMoved += (_, e) =>
        {
            if (!pinned && e.Pointer.PointerDeviceType == Microsoft.UI.Input.PointerDeviceType.Mouse)
                Inspect(Pick(e.GetCurrentPoint(canvas).Position));
        };
        PointerExited += (_, _) => { if (!pinned) Inspect(null); };
        Tapped += (_, e) =>
        {
            var reading = Pick(e.GetPosition(canvas));
            pinned = reading is not null && (!pinned || selected != reading);
            Inspect(pinned ? reading : null, announce: pinned);
        };
        KeyDown += HandleKey;
    }

    internal void RestoreInspection(UsageHistoryPlot previous)
    {
        if (previous.selected is not { } reading) return;
        int match = Array.FindIndex(options, candidate => candidate.Kind == reading.Kind &&
            (candidate.Kind == Kind.Forecast || candidate.Point == reading.Point));
        if (match < 0) return;
        index = match;
        selected = options[match];
        pinned = previous.pinned;
    }

    private void HandleKey(object sender, KeyRoutedEventArgs e)
    {
        if (options.Length == 0) return;
        switch (e.Key)
        {
            case VirtualKey.Left: index = Math.Max(0, index - 1); break;
            case VirtualKey.Right: index = Math.Min(options.Length - 1, index + 1); break;
            case VirtualKey.Home: index = 0; break;
            case VirtualKey.End: index = options.Length - 1; break;
            case VirtualKey.Enter:
            case VirtualKey.Space:
                pinned = !pinned;
                e.Handled = true;
                Inspect(pinned ? selected ?? options[index] : null, announce: true);
                return;
            case VirtualKey.Escape when selected is not null:
                pinned = false;
                e.Handled = true;
                Inspect(null);
                return;
            default: return;
        }
        pinned = false;
        e.Handled = true;
        Inspect(options[index], announce: true);
    }

    private void Draw()
    {
        canvas.Children.Clear();
        hitLines.Clear();
        right = ActualWidth - 14;
        bottom = ActualHeight - 29;
        if (right <= Left || bottom <= Top) return;
        foreach (int value in new[] { 100, 50, 0 })
        {
            double y = Y(value);
            DrawLine(new(Left, y), new(right, y), Rule);
            Mark(value + "%", Left - 7, y - 8, 1);
        }

        double now = X(model.Now);
        double reset = model.ResetAt > model.Now && model.ResetAt <= end ? X(model.ResetAt) : double.PositiveInfinity;
        double previous = double.NegativeInfinity;
        foreach (var (at, label) in new[] { (model.Start, range == "period" ? "Period start" : range == "week" ? "1 week ago" : "24 hours ago"),
            (model.Now - 86400d, "24 hours ago"), (model.Now - 3600d, "1 hour ago") })
        {
            double x = X(at);
            if (at < model.Start || x - previous < 90 || now - x < 50 || reset - x < 54) continue;
            previous = x;
            Guide(x, Subtle, [3, 5]);
            Mark(label, x, 13, at == model.Start ? 0 : 1);
        }
        double previousReset = double.NegativeInfinity;
        foreach (long at in model.Resets)
        {
            double x = X(at);
            Guide(x, Amber, [6, 4]);
            if (x - previousReset >= 40 && reset - x >= 40)
            {
                Mark("Reset", x, 0, x - Left < 30 ? 0 : 1, Amber);
                previousReset = x;
            }
        }
        Guide(now, Ink, []);
        Mark("Now", now, bottom + 7, .5, Ink);
        if (double.IsFinite(reset))
        {
            Guide(reset, Amber, [6, 4]);
            Mark("Reset", reset, 13, 1, Amber);
        }
        foreach (var (from, to) in model.GapConnectors)
            DrawLine(Pixel(from), Pixel(to), Accent, 2, [1, 5]);
        for (int i = 0; i < series.Length; ++i) DrawTrend(series[i].Trend, i);
        var projection = options.LastOrDefault(o => o.Kind == Kind.Forecast);
        if (projection is not null && model.Remaining is double remaining && projection.Point.At > model.Now)
        {
            var a = new Point(now, Y(remaining));
            var b = Pixel(projection.Point);
            DrawLine(a, b, Accent, 2, [5, 6]);
            hitLines.Add((a, b, -1));
        }
        foreach (var point in model.Milestones)
        {
            var marker = new Ellipse { Width = 8, Height = 8, Fill = Panel, Stroke = Accent, StrokeThickness = 1.7, IsHitTestVisible = false };
            var p = Pixel(point);
            Place(marker, p.X - 4, p.Y - 4);
        }
        canvas.Children.Add(cursor);
        canvas.Children.Add(dot);
        canvas.Children.Add(tip);
        Inspect(selected);
    }

    private void DrawTrend(UsageChartPoint[] trend, int seriesIndex)
    {
        if (trend.Length == 0) return;
        if (trend.Length == 1)
        {
            DrawReadingMarker(trend[0]);
            return;
        }
        var figure = new PathFigure { StartPoint = Pixel(trend[0]), IsFilled = false };
        double[] slopes = trend.Skip(1).Select((p, i) => (p.Remaining - trend[i].Remaining) / (p.At - trend[i].At)).ToArray();
        double Tangent(int i) => i == 0 ? slopes[0] : i == trend.Length - 1 ? slopes[^1] : Math.Max(slopes[i - 1], slopes[i]);
        for (int i = 1; i < trend.Length; ++i)
        {
            var p = trend[i - 1];
            var q = trend[i];
            double dt = (q.At - p.At) / 3;
            Point a = Pixel(p), b = new(X(p.At + dt), Y(p.Remaining + Tangent(i - 1) * dt)),
                c = new(X(q.At - dt), Y(q.Remaining - Tangent(i) * dt)), d = Pixel(q);
            figure.Segments.Add(new BezierSegment { Point1 = b, Point2 = c, Point3 = d });
            // Cache a short piecewise-linear approximation for pointer distance in DIPs.
            Point last = a;
            for (int j = 1; j <= 8; ++j)
            {
                double t = j / 8d, u = 1 - t;
                Point next = new(u*u*u*a.X + 3*u*u*t*b.X + 3*u*t*t*c.X + t*t*t*d.X,
                    u*u*u*a.Y + 3*u*u*t*b.Y + 3*u*t*t*c.Y + t*t*t*d.Y);
                hitLines.Add((last, next, seriesIndex));
                last = next;
            }
        }
        var geometry = new PathGeometry();
        geometry.Figures.Add(figure);
        canvas.Children.Add(new Path { Data = geometry, Stroke = Accent, StrokeThickness = 2.5, StrokeLineJoin = PenLineJoin.Round, IsHitTestVisible = false });
    }

    private Reading? Pick(Point p)
    {
        if (p.X < Left - HitRadius || p.X > right + HitRadius || p.Y < Top - HitRadius || p.Y > bottom + HitRadius) return null;
        Reading? nearest = null;
        double best = HitRadius * HitRadius;
        foreach (var reading in options)
        {
            double distance = DistanceSquared(p, Pixel(reading.Point));
            if (distance >= best) continue;
            best = distance;
            nearest = reading;
        }
        if (nearest is not null) return nearest;

        // Dotted gap connectors are not selectable evidence. A hit on a smoothed curve
        // inspects a real reading in the same segment, not the displayed interpolation.
        int hit = -2;
        foreach (var (a, b, segment) in hitLines)
        {
            double dx = b.X - a.X, dy = b.Y - a.Y, length = dx*dx + dy*dy;
            double t = length <= 0 ? 0 : Math.Clamp(((p.X-a.X)*dx + (p.Y-a.Y)*dy) / length, 0, 1);
            double distance = DistanceSquared(p, new Point(a.X + t*dx, a.Y + t*dy));
            if (distance >= best) continue;
            best = distance;
            hit = segment;
        }
        if (hit == -1) return options.LastOrDefault(o => o.Kind == Kind.Forecast);
        if (hit < 0) return null;
        return new Reading(series[hit].Raw.MinBy(o => DistanceSquared(p, Pixel(o)))!, Kind.Recorded);
    }

    private void DrawReadingMarker(UsageChartPoint point)
    {
        var p = Pixel(point);
        Place(new Ellipse { Width = 6, Height = 6, Fill = Accent, IsHitTestVisible = false }, p.X - 3, p.Y - 3);
    }

    private void Inspect(Reading? reading, bool announce = false)
    {
        selected = reading;
        var visibility = reading is null ? Visibility.Collapsed : Visibility.Visible;
        tip.Visibility = cursor.Visibility = dot.Visibility = visibility;
        if (reading is null || right <= Left || bottom <= Top) return;
        var p = Pixel(reading.Point);
        cursor.X1 = cursor.X2 = p.X;
        cursor.Y1 = Top;
        cursor.Y2 = bottom;
        Canvas.SetLeft(dot, p.X - 5);
        Canvas.SetTop(dot, p.Y - 5);
        string kind = reading.Kind switch
        {
            Kind.Forecast => "Projected · Recent average",
            Kind.Threshold => model.Ledger ? "Recorded threshold crossing" : "Approximate threshold crossing",
            _ => "Recorded"
        };
        var local = DateTimeOffset.FromUnixTimeSeconds((long)reading.Point.At).ToLocalTime();
        tipText.Text = $"{reading.Point.Remaining:0.#}% remaining · {local:g}\n{kind}" + (pinned ? " · pinned" : "");
        tip.MaxWidth = Math.Max(1, Math.Min(290, ActualWidth - 8));
        tip.Measure(new Size(tip.MaxWidth, double.PositiveInfinity));
        double tipX = Math.Max(4, Math.Min(ActualWidth - tip.DesiredSize.Width - 4, p.X - tip.DesiredSize.Width / 2));
        double tipY = p.Y - tip.DesiredSize.Height - 10;
        if (tipY < 4) tipY = p.Y + 10;
        Canvas.SetLeft(tip, tipX);
        Canvas.SetTop(tip, Math.Max(4, Math.Min(ActualHeight - tip.DesiredSize.Height - 4, tipY)));
        if (announce) FrameworkElementAutomationPeer.FromElement(tipText)?.RaiseAutomationEvent(AutomationEvents.LiveRegionChanged);
    }

    private double X(double at) => Left + (at - model.Start) / (end - model.Start) * (right - Left);
    private double Y(double remaining) => bottom - remaining / 100 * (bottom - Top);
    private Point Pixel(UsageChartPoint p) => new(X(p.At), Y(p.Remaining));
    private static double DistanceSquared(Point a, Point b) => (a.X-b.X)*(a.X-b.X) + (a.Y-b.Y)*(a.Y-b.Y);
    private void Guide(double x, Brush brush, double[] dash) => DrawLine(new(x, Top - 4), new(x, bottom), brush, 1, dash);
    private void DrawLine(Point a, Point b, Brush brush, double thickness = 1, double[]? dash = null)
    {
        var line = new Line { X1 = a.X, X2 = b.X, Y1 = a.Y, Y2 = b.Y, Stroke = brush, StrokeThickness = thickness, IsHitTestVisible = false };
        if (dash is { Length: > 0 })
        {
            var pattern = new DoubleCollection();
            foreach (double n in dash) pattern.Add(n);
            line.StrokeDashArray = pattern;
        }
        canvas.Children.Add(line);
    }

    private void Mark(string text, double x, double y, double anchor, Brush? color = null)
    {
        var label = Label(text, 11, color);
        label.TextWrapping = TextWrapping.NoWrap;
        label.IsHitTestVisible = false;
        AutomationProperties.SetAccessibilityView(label, AccessibilityView.Raw);
        label.Measure(new Size(double.PositiveInfinity, double.PositiveInfinity));
        Place(label, Math.Clamp(x - anchor * label.DesiredSize.Width, 0, Math.Max(0, ActualWidth - label.DesiredSize.Width)), y);
    }

    private void Place(UIElement element, double x, double y)
    {
        Canvas.SetLeft(element, x);
        Canvas.SetTop(element, y);
        canvas.Children.Add(element);
    }

    internal static TextBlock Label(string text, double size, Brush? color = null) => new()
    {
        Text = text, FontSize = size, Foreground = color ?? Subtle, TextWrapping = TextWrapping.Wrap
    };
    private static SolidColorBrush Brush(byte r, byte g, byte b) => new(Windows.UI.Color.FromArgb(255, r, g, b));
}
