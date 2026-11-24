using System.Globalization;
using Agent.Core;
using Microsoft.UI.Input;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Automation.Peers;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
using Windows.System;
using Windows.UI;
using Windows.UI.Core;

namespace Agent.App;

public sealed partial class MainWindow
{
    private const string NewConversationTitle = "New conversation";
    private static readonly SolidColorBrush Muted = new(Color.FromArgb(255, 163, 174, 166));
    private static readonly SolidColorBrush SelectedConversation = new(Color.FromArgb(255, 44, 57, 48));
    private IReadOnlyList<SavedConversation> savedConversations = [];
    private string chatTitle = NewConversationTitle;
    private bool settingsOpen, compactSidebar, sidebarOpen;
    private bool historyLoaded;

    private void WireChatLayout()
    {
        var workspace = C<Grid>("Workspace");
        workspace.SizeChanged += (_, e) => ResizeSidebar(e.NewSize.Width);
        Hook("SidebarToggle", () => { SetSidebarOpen(!sidebarOpen, true); return Task.CompletedTask; }, false);
        Hook("SidebarBackdrop", () => { SetSidebarOpen(false, true); return Task.CompletedTask; }, false);
        workspace.PreviewKeyDown += (_, e) =>
        {
            if (!sidebarOpen || e.Key != VirtualKey.Escape) return;
            e.Handled = true;
            SetSidebarOpen(false, true);
        };

        var composer = C<TextBox>("Composer");
        bool composing = false;
        composer.TextCompositionStarted += (_, _) => composing = true;
        composer.TextCompositionEnded += (_, _) => composing = false;
        composer.LostFocus += (_, _) => composing = false;
        composer.PreviewKeyDown += async (_, e) =>
        {
            if (e.Key != VirtualKey.Enter || composing || IsKeyDown(VirtualKey.Shift)) return;
            e.Handled = true;
            if (e.KeyStatus.WasKeyDown || busy || string.IsNullOrWhiteSpace(composer.Text)) return;
            long epoch = accountEpoch;
            await GuardAsync(SendAsync);
            if (!closed && client is not null && epoch == accountEpoch && !settingsOpen && !sidebarOpen) composer.Focus(FocusState.Programmatic);
        };
        C<Grid>("ComposerToolbar").SizeChanged += (_, e) =>
        {
            bool stacked = e.NewSize.Width < 390;
            var research = C<CheckBox>("ResearchToggle");
            Grid.SetRow(research, stacked ? 1 : 0);
            Grid.SetColumn(research, stacked ? 0 : 1);
            Grid.SetColumnSpan(research, stacked ? 2 : 1);
            Grid.SetRow(C<Button>("Send"), stacked ? 1 : 0);
        };
        C<ScrollViewer>("MessageScroll").SizeChanged += (_, e) =>
            C<StackPanel>("Welcome").Margin = new Thickness(0, Math.Clamp(e.NewSize.Height * .17, 0, 120), 0, 0);
        C<TextBox>("HistorySearch").TextChanged += (_, _) => { if (!rendering) RenderConversationHistory(); };
        C<ListView>("History").ItemClick += async (_, e) =>
        {
            if (busy || rendering || e.ClickedItem is not ListViewItem { Tag: string id }) return;
            await GuardAsync(() => LoadConversationAsync(id));
        };
    }

    private static bool IsKeyDown(VirtualKey key) =>
        (InputKeyboardSource.GetKeyStateForCurrentThread(key) & CoreVirtualKeyStates.Down) != 0;

    private void ResizeSidebar(double width)
    {
        compactSidebar = width < 700;
        if (!compactSidebar) sidebarOpen = false;
        C<Grid>("Workspace").ColumnDefinitions[0].Width = new GridLength(compactSidebar ? 0 : 232);
        var sidebar = C<Border>("Sidebar");
        Grid.SetRow(sidebar, compactSidebar ? 1 : 0);
        Grid.SetRowSpan(sidebar, compactSidebar ? 1 : 2);
        Grid.SetColumnSpan(sidebar, compactSidebar ? 2 : 1);
        UpdateSidebar();
    }

    private void UpdateSidebar()
    {
        var toggle = C<Button>("SidebarToggle");
        var sidebar = C<Border>("Sidebar");
        toggle.Visibility = compactSidebar ? Visibility.Visible : Visibility.Collapsed;
        string label = sidebarOpen ? "Close conversations" : "Open conversations";
        AutomationProperties.SetName(toggle, label);
        ToolTipService.SetToolTip(toggle, label);
        C<ContentControl>("WorkspaceContent").IsEnabled = !sidebarOpen;
        sidebar.Visibility = !compactSidebar || sidebarOpen ? Visibility.Visible : Visibility.Collapsed;
        C<Button>("SidebarBackdrop").Visibility = sidebarOpen ? Visibility.Visible : Visibility.Collapsed;
    }

    private void SetSidebarOpen(bool show, bool moveFocus = false)
    {
        if (!compactSidebar) return;
        sidebarOpen = show;
        UpdateSidebar();
        if (moveFocus)
        {
            Control target = show ? C<TextBox>("HistorySearch") : C<Button>("SidebarToggle");
            target.Focus(FocusState.Programmatic);
        }
    }

    private void RenderAccountName(string name)
    {
        C<TextBlock>("SignedInAs").Text = name;
        C<TextBlock>("AccountInitial").Text = name.Length == 0 ? "" : StringInfo.GetNextTextElement(name).ToUpperInvariant();
    }

    private void ShowSettings(bool show)
    {
        settingsOpen = show;
        C<Grid>("ChatView").Visibility = show ? Visibility.Collapsed : Visibility.Visible;
        C<ScrollViewer>("SettingsView").Visibility = show ? Visibility.Visible : Visibility.Collapsed;
        var button = C<Button>("SettingsNav");
        string label = show ? "Back to chat" : "Settings";
        ((SymbolIcon)button.Content).Symbol = show ? Symbol.Back : Symbol.Setting;
        AutomationProperties.SetName(button, label);
        ToolTipService.SetToolTip(button, label);
        RenderConversationTitle();
        SetSidebarOpen(false, sidebarOpen);
    }

    private void RenderConversationTitle() => C<TextBlock>("ConversationTitle").Text = settingsOpen ? "Settings" : chatTitle;

    private void RenderConversationHistory()
    {
        var history = C<ListView>("History");
        string query = C<TextBox>("HistorySearch").Text;
        var rows = ConversationHistory.Filter(savedConversations, query, DateTimeOffset.Now, TimeZoneInfo.Local).ToArray();
        history.Items.Clear();
        foreach (var group in rows.GroupBy(row => row.Group))
        {
            var heading = new TextBlock { Text = group.Key, Foreground = Muted, FontSize = 11,
                FontWeight = Microsoft.UI.Text.FontWeights.SemiBold, Margin = new Thickness(0, 8, 0, 4) };
            AutomationProperties.SetHeadingLevel(heading, AutomationHeadingLevel.Level2);
            history.Items.Add(new ListViewItem { Content = heading, IsEnabled = false, IsTabStop = false, MinHeight = 28 });
            foreach (var row in group)
            {
                var title = new TextBlock { Text = row.Title, FontSize = 12, TextWrapping = TextWrapping.Wrap,
                    MaxLines = 2, TextTrimming = TextTrimming.CharacterEllipsis };
                var content = new StackPanel { Spacing = 4 };
                content.Children.Add(title);
                string date = row.LocalDate?.ToString("MMM d", CultureInfo.CurrentCulture) ?? "";
                if (date.Length != 0) content.Children.Add(new TextBlock { Text = date, FontSize = 10, Foreground = Muted });
                var entry = new ListViewItem { Content = content, Tag = row.Id, Padding = new Thickness(10, 10, 6, 10),
                    HorizontalContentAlignment = HorizontalAlignment.Stretch };
                AutomationProperties.SetName(entry, date.Length == 0 ? row.Title : row.Title + ", " + date);
                ToolTipService.SetToolTip(entry, row.Title);
                history.Items.Add(entry);
            }
        }
        C<TextBlock>("HistoryEmpty").Text = historyLoaded && rows.Length == 0
            ? (string.IsNullOrWhiteSpace(query) ? "No saved conversations yet." : "No matching conversations.") : "";
        UpdateActiveConversation();
    }

    private void UpdateActiveConversation()
    {
        foreach (var item in C<ListView>("History").Items.OfType<ListViewItem>())
        {
            if (item.Tag is not string id) continue;
            bool active = id == chat.ConversationId;
            if (active) item.Background = SelectedConversation;
            else item.ClearValue(Control.BackgroundProperty);
            AutomationProperties.SetItemStatus(item, active ? "Current conversation" : "");
        }
    }
}
