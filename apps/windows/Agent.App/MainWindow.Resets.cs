using Agent.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using System.Text.Json.Nodes;

namespace Agent.App;

public sealed partial class MainWindow
{
    private ResetCredits? resetCredits;

    private void WireResets()
    {
        Hook("UseReset", ConfirmResetAsync, false);
        // The allowance refresh control follows the existing serialized Settings gate.
        C<Button>("RefreshAllowance").RegisterPropertyChangedCallback(Control.IsEnabledProperty, (_, _) => UpdateResetAction());
        C<ScrollViewer>("SettingsView").RegisterPropertyChangedCallback(UIElement.VisibilityProperty, (_, _) =>
        {
            if (!settingsOpen) RenderResets(acceptStatus: false);
        });
    }

    private void UpdateResetAction() => C<Button>("UseReset").IsEnabled = !busy && settingsOpen && device is null &&
        resetCredits?.EligibleOffer(DateTimeOffset.UtcNow.ToUnixTimeSeconds()) is not null;

    private void InvalidateResetBalance()
    {
        if (usage is not JsonObject previousUsage) return;
        previousUsage.Remove("resets");
        if (previousUsage["snapshot"] is JsonObject snapshot) snapshot["stale"] = true;
    }

    private void RenderResets(bool acceptStatus = true)
    {
        var credits = acceptStatus && settingsOpen ? ResetCredits.Read(usage, activeProfile, connectionId) : null;
        resetCredits = credits;
        C<Border>("ResetsSection").Visibility = settingsOpen && activeProfile.Length != 0 ? Visibility.Visible : Visibility.Collapsed;
        SetSettingsText("ResetNotice", "");
        SetSettingsText("ResetBalance", credits is null ? "Reset balance unavailable" :
            $"{credits.AvailableCount} {(credits.AvailableCount == 1 ? "reset" : "resets")} available");
        SetSettingsText("ResetDetails", credits is { AvailableCount: > 0, DetailsComplete: false }
            ? "Some reset details are unavailable. Refresh allowance to check again." : "");

        var expirations = C<StackPanel>("ResetExpirations");
        expirations.Children.Clear();
        var history = C<Expander>("ResetHistory");
        var events = C<StackPanel>("ResetEvents");
        events.Children.Clear();
        if (credits is null) history.IsExpanded = false;
        else
        {
            long now = DateTimeOffset.UtcNow.ToUnixTimeSeconds();
            foreach (var expiry in credits.Expirations.Where(x => x.ExpiresAt is null || x.ExpiresAt > now))
                expirations.Children.Add(Text($"{expiry.Count} {(expiry.Count == 1 ? "reset expires" : "resets expire")} · " +
                    (expiry.ExpiresAt is long at ? ResetTime(at) : "Expiration unavailable"), 12));
            foreach (var item in credits.History)
            {
                var row = new StackPanel { Spacing = 2 };
                var label = Text(item.Kind == "used" ? "1 reset used" : "1 reset expired");
                label.FontWeight = Microsoft.UI.Text.FontWeights.SemiBold;
                row.Children.Add(label);
                row.Children.Add(Text(ResetTime(item.At), 12));
                events.Children.Add(row);
            }
        }
        events.Children.Add(Text(credits is null || credits.HistoryUnavailable ? "Recorded reset history is unavailable." :
            credits.History.Count == 0 ? "No reset events recorded by Agent yet." : "Reset events recorded by Agent.", 12));
        UpdateResetAction();
    }

    private static string ResetTime(long seconds) => DateTimeOffset.FromUnixTimeSeconds(seconds).ToLocalTime().ToString("g") + " (local time)";

    private async Task ConfirmResetAsync()
    {
        if (busy || !settingsOpen || device is not null || currentDialog is not null || client is not { } captured ||
            resetCredits is null) return;
        if (resetCredits.EligibleOffer(DateTimeOffset.UtcNow.ToUnixTimeSeconds()) is not { } offer)
        {
            UpdateResetAction();
            SetSettingsText("ResetNotice", "The next reset is no longer available. Refresh allowance to check again.");
            C<Expander>("ResetHistory").Focus(FocusState.Programmatic);
            return;
        }

        long epoch = accountEpoch;
        string profile = activeProfile, connection = connectionId;
        if (C<ComboBox>("Accounts").SelectedItem is not ComboBoxItem item ||
            item.Tag is not JsonNode account || account.Text("id") != profile ||
            item.Content is not string accountLabel) return;

        bool OwnsScope() => IsCurrentSession(captured, epoch) && settingsOpen && profile == activeProfile && connection == connectionId;
        bool OwnsReset() => OwnsScope() && device is null &&
            resetCredits?.EligibleOffer(DateTimeOffset.UtcNow.ToUnixTimeSeconds()) == offer;

        int remaining = offer.AvailableCount - 1;
        string expiry = offer.Credit.ExpiresAt is long at ? "that expires " + ResetTime(at) : "whose expiration is unavailable";
        var dialog = new ContentDialog
        {
            Title = "Use 1 reset?",
            Content = Text($"This will use the reset {expiry} for {accountLabel}. You’ll have {remaining} {(remaining == 1 ? "reset" : "resets")} left. This can’t be undone."),
            PrimaryButtonText = "Use reset",
            CloseButtonText = "Cancel",
            DefaultButton = ContentDialogButton.Close,
            XamlRoot = root.XamlRoot,
            RequestedTheme = ElementTheme.Dark
        };
        currentDialog = dialog;
        try
        {
            ContentDialogResult choice = await dialog.ShowAsync();
            if (choice != ContentDialogResult.Primary) return;
            await GuardAsync(async () =>
            {
                if (!OwnsReset())
                {
                    if (OwnsScope()) SetSettingsText("ResetNotice", "The confirmed reset is no longer current. Refresh allowance to check again.");
                    return;
                }
                SetSettingsText("ResetNotice", "Using reset…");
                try
                {
                    using var deadline = CancellationTokenSource.CreateLinkedTokenSource(accountLifetime.Token);
                    deadline.CancelAfter(TimeSpan.FromSeconds(25));
                    var reply = await ResetCredits.ConsumeAsync(captured, profile, connection, offer, deadline.Token);
                    if (!OwnsScope()) return;
                    usage = reply.Status;
                    RenderAllowance();
                    SetSettingsText("ResetNotice", reply.Outcome switch
                    {
                        "alreadyRedeemed" => "That reset was already used. Review the updated balance.",
                        "nothingToReset" => "There was no allowance to reset.",
                        "noCredit" => "That reset is no longer available. Review the updated balance.",
                        _ => ""
                    });
                }
                catch (SessionExpiredException) { throw; }
                catch (Exception error)
                {
                    if (!OwnsScope()) return;
                    bool ambiguous = error is not AgentException { Status: >= 400 and < 500 and not 408 };
                    InvalidateResetBalance();
                    RenderAllowance();
                    SetSettingsText("ResetNotice", ambiguous
                        ? "Couldn’t confirm the reset result. Refresh the ChatGPT connection before trying again. The request was not retried."
                        : "The reset request was declined. Refresh the ChatGPT connection and review the available resets.");
                }
            });
        }
        finally
        {
            if (currentDialog == dialog) currentDialog = null;
            if (OwnsScope())
            {
                UpdateResetAction();
                var button = C<Button>("UseReset");
                if (!button.IsEnabled || !button.Focus(FocusState.Programmatic))
                    C<Expander>("ResetHistory").Focus(FocusState.Programmatic);
            }
        }
    }
}
