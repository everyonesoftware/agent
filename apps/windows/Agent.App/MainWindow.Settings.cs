using System.Text;
using System.Text.Json.Nodes;
using Agent.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Media;
namespace Agent.App;
public sealed partial class MainWindow
{
    private CancellationTokenSource? openingSettingsCancellation;
    private Task? openingSettings;

    private async Task LoadOpeningSettingsAsync()
    {
        using var refresh = CancellationTokenSource.CreateLinkedTokenSource(accountLifetime.Token);
        openingSettingsCancellation = refresh;
        try
        {
            await GuardAsync(async () =>
            {
                try { await RefreshSettingsAsync(refresh.Token); }
                catch (OperationCanceledException) when (refresh.IsCancellationRequested) { }
            });
        }
        finally { openingSettingsCancellation = null; }
    }

    private async Task LeaveSettingsAsync()
    {
        if (openingSettings is { IsCompleted: false } opening)
        {
            if (openingSettingsCancellation is not { IsCancellationRequested: false }) return;
            openingSettingsCancellation?.Cancel();
            await opening;
        }
        if (!closed && settingsOpen && !busy) NavigateSettings(false);
    }

    private void WireSettingsLayout()
    {
        var groups = new[] { C<StackPanel>("ServerActions"), C<StackPanel>("AccountActions"),
            C<StackPanel>("DeviceActions"), C<StackPanel>("BraveActions"), C<StackPanel>("MemoryActions") };
        var card = C<Border>("ConnectionSection");
        C<Grid>("SettingsColumn").SizeChanged += (sender, e) =>
        {
            var column = (Grid)sender;
            double available = e.NewSize.Width - column.Padding.Left - column.Padding.Right
                - card.Padding.Left - card.Padding.Right - card.BorderThickness.Left - card.BorderThickness.Right;
            var orientation = e.NewSize.Width < 520 ? Orientation.Vertical : Orientation.Horizontal;
            foreach (var group in groups)
            {
                // Bound each label even in a horizontal group, where StackPanel measures children at infinite width.
                double limit = orientation == Orientation.Vertical ? double.PositiveInfinity
                    : Math.Max(0, (available - (group.Children.Count - 1) * group.Spacing) / group.Children.Count);
                foreach (var action in group.Children.OfType<Button>()) action.MaxWidth = limit;
                group.Orientation = orientation;
            }
        };
    }

    private void SetSettingsText(string name, string text)
    {
        var node = C<TextBlock>(name);
        node.Text = text;
        node.Visibility = string.IsNullOrEmpty(text) ? Visibility.Collapsed : Visibility.Visible;
    }

    private void SetConnectionBadge(string status, bool connected = false)
    {
        var badge = C<TextBlock>("ConnectionBadge");
        badge.Text = status + "  ↻";
        if (connected) badge.Foreground = Sage;
        else badge.ClearValue(TextBlock.ForegroundProperty);
        AutomationProperties.SetName(C<Button>("RefreshConnection"), "Refresh ChatGPT connection status: " + status);
    }

    private async Task RefreshSettingsAsync(CancellationToken? cancellation = null)
    {
        var c=Need();long epoch=accountEpoch;var user=await c.GetAsync("/api/auth/me",cancellation ?? accountLifetime.Token);cancellation?.ThrowIfCancellationRequested();if(!IsCurrentSession(c,epoch))return;
        RenderAccountName(user.Text("username"));C<TextBlock>("ServerDescription").Text=c.Endpoint.Authority;accountView.MarkLoaded(SettingsSection.Account);
        // Each read reports its own failure; unavailable ChatGPT must not hide memory or research.
        foreach(var load in new Func<CancellationToken?, Task>[]{RefreshProviderAsync,RefreshResearchAsync,RefreshMemoryAsync})
        {
            if(!IsCurrentSession(c,epoch))return;
            try{await load(cancellation);}catch(SessionExpiredException){throw;}catch(OperationCanceledException){throw;}catch{cancellation?.ThrowIfCancellationRequested();if(IsCurrentSession(c,epoch))Notify("Some Settings data is unavailable. Refresh the affected section to retry.");}
        }
    }
    private async Task RefreshProviderAsync(CancellationToken? cancellation = null)
    {
        var c=Need();long epoch=accountEpoch;SetConnectionBadge("Checking…");
        try
        {
            var token = cancellation ?? accountLifetime.Token;
            var status=await c.GetAsync("/api/auth/status",token);var profiles=await c.GetAsync("/api/auth/profiles",token);cancellation?.ThrowIfCancellationRequested();if(!IsCurrentSession(c,epoch))return;
            if(accountView.IsLoaded(SettingsSection.Provider)&&activeProfile!=profiles.Text("active_profile_id"))
            {
                ResetAccountView();await RefreshWorkspaceAsync();await RefreshSettingsAsync();return;
            }
            activeProfile=profiles.Text("active_profile_id");usage=null;connectionId="";accountView.MarkLoaded(SettingsSection.Provider);
            bool ready=status.Flag("connected")&&status.Flag("plan_usage_enabled");
            SetConnectionBadge(ready ? "Connected" : status.Flag("connected") ? "Permission required" : status.Flag("oauth_selected") ? "Sign-in required" : "Not connected", ready);
            SetSettingsText("ProviderMessage", ready ? "" : "Connect your ChatGPT account to Agent with Codex.");
            rendering=true;try
            {
                C<ComboBox>("Accounts").Items.Clear();foreach(var profile in profiles.Items("profiles"))
                {
                    var item=new ComboBoxItem{Content=profile.Text("label","ChatGPT account")+(profile.Text("email")==""?"":" · "+profile.Text("email"))+(profile.Flag("registration_pending")?" · finish sign-in":profile.Flag("plan_usage_enabled")?" · connected":" · disconnected"),Tag=profile};
                    C<ComboBox>("Accounts").Items.Add(item);if(profile.Text("id")==activeProfile)C<ComboBox>("Accounts").SelectedItem=item;
                }
            }finally{rendering=false;}
            C<Button>("AddAccount").Visibility = C<ComboBox>("Accounts").Visibility = C<ComboBox>("Accounts").Items.Count == 0 ? Visibility.Collapsed : Visibility.Visible;
            C<Button>("DisconnectAccount").Visibility=activeProfile==""?Visibility.Collapsed:Visibility.Visible;C<Button>("ConnectChatGPT").Visibility=ready?Visibility.Collapsed:Visibility.Visible;
            if(activeProfile!="")await RefreshUsageAsync(false,cancellation);else RenderAllowance();await RefreshModelsAsync(cancellation);
        }
        catch (OperationCanceledException) when (cancellation?.IsCancellationRequested == true) { throw; }
        catch{if(IsCurrentSession(c,epoch)){SetConnectionBadge("Unavailable");SetSettingsText("ProviderMessage", "Couldn’t check the ChatGPT connection. Your saved accounts have not been removed. Refresh to retry.");}throw;}
    }
    private async Task RefreshUsageAsync(bool refresh, CancellationToken? cancellation = null)
    {
        if(activeProfile==""){RenderAllowance();return;}var c=Need();long epoch=accountEpoch;string profile=activeProfile;
        try
        {
            var result=refresh?await c.PostAsync("/api/usage/refresh",new{profile_id=profile,connection_id=connectionId},accountLifetime.Token):await c.GetAsync("/api/usage/status?profile_id="+Wire.Segment(profile),cancellation ?? accountLifetime.Token);
            cancellation?.ThrowIfCancellationRequested();if(!IsCurrentSession(c,epoch)||profile!=activeProfile)return;if(result.Text("profile_id")!=profile)throw new AgentException("Allowance account changed. Refresh connection status.");usage=result;connectionId=result.Text("connection_id");RenderAllowance();
            if(result["login"] is JsonNode login&&result.Text("state") is "pending" or "checking")RenderDevice(new JsonObject{["profile_id"]=profile,["connection_id"]=connectionId,["login_id"]=login.Text("login_id"),["verification_url"]=login.Text("verification_url"),["user_code"]=login.Text("user_code")});
        }
        catch (OperationCanceledException) when (cancellation?.IsCancellationRequested == true) { throw; }
        catch{if(IsCurrentSession(c,epoch)&&profile==activeProfile){if(usage?["snapshot"] is JsonObject snapshot)snapshot["stale"]=true;RenderAllowance();SetSettingsText("AllowanceMessage", "Refresh failed. "+(usage?["snapshot"] is null?"No allowance observation is available.":"Showing last-known values; they may no longer be current."));}throw;}
    }
    private void RenderAllowance()
    {
        var panel=C<StackPanel>("AllowanceWindows");panel.Children.Clear();bool stale=usage?["snapshot"].Flag("stale")??false;
        SetSettingsText("AllowanceMessage", stale?"Showing last-known allowance; these values may no longer be current.":usage is null?"Connect ChatGPT above to see allowance.":usage.Text("state")=="connected"?"":usage.Text("state","Unavailable"));
        long fetched=usage?["snapshot"]?["fetched_at"]?.GetValue<long>()??0;
        SetSettingsText("AllowanceFreshness", fetched > 0
            ? "Updated " + DateTimeOffset.FromUnixTimeSeconds(fetched).ToLocalTime().ToString("g") + (stale ? " · stale" : "")
            : usage?["snapshot"] is null ? "" : "Update time unavailable");
        foreach(var window in usage?["snapshot"].Items("windows")??[])
        {
            double? remaining=ClientPresentation.Remaining(window["used_percent"]?.GetValue<double>());long minutes=window["window_minutes"]?.GetValue<long>()??0,reset=window["reset_at"]?.GetValue<long>()??0;
            string label=minutes==300?"5-hour window":minutes==10080?"Weekly window":window.Text("label","Allowance window");
            var row=new StackPanel{Spacing=6};row.Children.Add(Text((remaining is null?"Unavailable":$"{remaining:0.#}% remaining")+" · "+label));
            row.Children.Add(Text(reset>DateTimeOffset.UtcNow.ToUnixTimeSeconds()?"Resets "+DateTimeOffset.FromUnixTimeSeconds(reset).ToLocalTime().ToString("g"):"Reset time unavailable",12));
            if(remaining is not null){var meter=new ProgressBar{Minimum=0,Maximum=100,Value=remaining.Value,Foreground=Sage};Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(meter,label+" remaining"+(stale?" (last known)":""));row.Children.Add(meter);}panel.Children.Add(row);
        }
        if(usage?["snapshot"] is not null&&panel.Children.Count==0)panel.Children.Add(Text("Allowance windows are unavailable. Refresh to try again."));
        panel.Visibility = panel.Children.Count == 0 ? Visibility.Collapsed : Visibility.Visible;
    }
    private async Task StartDeviceAsync(string path)
    {
        accountView.Require(SettingsSection.Provider);if(device is not null)throw new AgentException("Finish or cancel the current ChatGPT sign-in first.");
        var c=Need();long epoch=accountEpoch;var result=await c.PostAsync(path,new{expected_active_profile_id=activeProfile},accountLifetime.Token);if(!IsCurrentSession(c,epoch))return;
        if(result.Flag("connected")){ResetAccountView();await RefreshWorkspaceAsync();await RefreshSettingsAsync();return;}RenderDevice(result);
    }
    private void RenderDevice(JsonNode result)
    {
        ClientPresentation.VerificationUri(result.Text("verification_url"));if(result.Text("login_id")==""||result.Text("profile_id")=="")throw new AgentException("The server returned an incomplete device sign-in.");
        device=result;C<StackPanel>("DeviceLogin").Visibility=Visibility.Visible;C<TextBlock>("DeviceCode").Text=result.Text("user_code");
    }
    private async Task PollDeviceAsync()
    {
        var ceremony=device;if(ceremony is null)return;var c=Need();long epoch=accountEpoch;
        var result=await c.GetAsync("/api/usage/status?profile_id="+Wire.Segment(ceremony.Text("profile_id"))+"&connection_id="+Wire.Segment(ceremony.Text("connection_id")),accountLifetime.Token);
        if(!IsCurrentSession(c,epoch)||ceremony!=device)return;
        if(result.Text("state")=="connected"){ResetAccountView();await RefreshWorkspaceAsync();await RefreshSettingsAsync();Notify("Connected to ChatGPT with Codex.",false);}
        else Notify(result.Text("state") is "pending" or "checking"?"Waiting for OpenAI approval.":"Sign-in is no longer pending. Cancel and refresh to retry.",false);
    }
    private async Task CancelDeviceAsync()
    {
        var ceremony=device;if(ceremony is null)return;var c=Need();long epoch=accountEpoch;
        await c.PostAsync("/api/usage/cancel",new{profile_id=ceremony.Text("profile_id"),connection_id=ceremony.Text("connection_id"),login_id=ceremony.Text("login_id")},accountLifetime.Token);
        if(!IsCurrentSession(c,epoch)||ceremony!=device)return;device=null;C<StackPanel>("DeviceLogin").Visibility=Visibility.Collapsed;await RefreshProviderAsync();Notify("Sign-in cancelled. Your existing active account is unchanged.",false);
    }
    private async Task ChangeProfileAsync(string profile,bool disconnect)
    {
        accountView.Require(SettingsSection.Provider);if(profile==""||device is not null)return;var c=Need();long epoch=accountEpoch;
        var password=new PasswordBox{Header="Agent password (not your ChatGPT password)"};var panel=new StackPanel{Spacing=12};panel.Children.Add(Text(disconnect?"Your saved conversations and subscription are kept. Tokens are cleared on this server; remote revocation may not be confirmed.":"Confirm the change of active ChatGPT account."));panel.Children.Add(password);
        try
        {
            if(await AskAsync(disconnect?"Disconnect ChatGPT?":"Switch ChatGPT account?",panel,disconnect?"Disconnect":"Switch account")!=ContentDialogResult.Primary)return;
            if(!IsCurrentSession(c,epoch))return;string fresh=password.Password;password.Password="";
            var result=await c.PostAsync(disconnect?"/api/auth/disconnect":"/api/auth/profiles/active",new{password=fresh,profile_id=profile},accountLifetime.Token);if(!IsCurrentSession(c,epoch))return;
            ResetAccountView();await RefreshWorkspaceAsync();await RefreshSettingsAsync();
            Notify(disconnect&&!result.Flag("remote_revocation_confirmed")?"Tokens were cleared here, but remote revocation was not confirmed. Revoke this app in ChatGPT settings.":disconnect?"ChatGPT disconnected.":"Active ChatGPT account changed.",false);
        }
        finally{password.Password="";rendering=true;try{C<ComboBox>("Accounts").SelectedItem=C<ComboBox>("Accounts").Items.OfType<ComboBoxItem>().FirstOrDefault(i=>(i.Tag as JsonNode).Text("id")==activeProfile);}finally{rendering=false;}}
    }
    private async Task RefreshResearchAsync(CancellationToken? cancellation = null)
    {
        var c=Need();long epoch=accountEpoch;var result=await c.GetAsync("/api/research/settings",cancellation ?? accountLifetime.Token);cancellation?.ThrowIfCancellationRequested();if(!IsCurrentSession(c,epoch))return;researchSettings=result["settings"]?.DeepClone()??throw new AgentException("Research settings are unavailable.");accountView.MarkLoaded(SettingsSection.Research);
        C<TextBlock>("ResearchStatus").Text=researchSettings.Flag("configured")?researchSettings.Text("status")+" · Key ending "+researchSettings.Text("key_suffix"):"Not connected";
        var budget=researchSettings["budget"];rendering=true;try{C<ToggleSwitch>("BudgetEnabled").IsOn=budget.Flag("enabled");C<NumberBox>("BudgetMaximum").Value=budget?["maximum"]?.GetValue<int>()??1;C<TextBox>("BudgetTimezone").Text=budget.Text("timezone");C<ComboBox>("BudgetPeriod").SelectedItem=C<ComboBox>("BudgetPeriod").Items.OfType<ComboBoxItem>().FirstOrDefault(i=>(string)i.Content==budget.Text("period"));}finally{rendering=false;}
    }
    private async Task ChangeResearchAsync(string action)
    {
        accountView.Require(SettingsSection.Research);var c=Need();long epoch=accountEpoch;string revision=researchSettings.Text("revision");
        string key=C<PasswordBox>("BraveKey").Password;C<PasswordBox>("BraveKey").Password="";
        object payload=action=="verify"?new{expected_revision=revision,api_key=key}:new{expected_revision=revision};
        await c.PostAsync("/api/research/settings/"+action,payload,accountLifetime.Token);if(IsCurrentSession(c,epoch)){await RefreshResearchAsync();Notify("Research connection updated. No task consent has been granted.",false);}
    }
    private async Task SaveBudgetAsync()
    {
        accountView.Require(SettingsSection.Research);var c=Need();long epoch=accountEpoch;
        string period=(C<ComboBox>("BudgetPeriod").SelectedItem as ComboBoxItem)?.Content as string??"";double maximum=C<NumberBox>("BudgetMaximum").Value;if(!double.IsFinite(maximum)||maximum<1||maximum%1!=0)throw new ArgumentException("Enter a whole-number search budget.");
        await c.PostAsync("/api/research/settings/budget",new{expected_revision=researchSettings.Text("revision"),expected_budget_revision=researchSettings?["budget"].Text("revision"),enabled=C<ToggleSwitch>("BudgetEnabled").IsOn,maximum=(int)maximum,period,timezone=C<TextBox>("BudgetTimezone").Text},accountLifetime.Token);if(IsCurrentSession(c,epoch))await RefreshResearchAsync();
    }
    private async Task RefreshMemoryAsync(CancellationToken? cancellation = null)
    {
        var c=Need();long epoch=accountEpoch;var result=await c.GetAsync("/api/memories",cancellation ?? accountLifetime.Token);cancellation?.ThrowIfCancellationRequested();if(!IsCurrentSession(c,epoch))return;accountView.MarkLoaded(SettingsSection.Memory);
        rendering=true;try{C<ToggleSwitch>("MemoryEnabled").IsOn=result.Flag("enabled");}finally{rendering=false;}
        var panel=C<StackPanel>("MemoryEntries");panel.Children.Clear();foreach(var entry in result.Items("entries"))
        {
            var row=new StackPanel{Spacing=6};row.Children.Add(Text(entry.Text("category"),12));row.Children.Add(Text(entry.Text("content")));var actions=new StackPanel{Orientation=Orientation.Horizontal,Spacing=8};var edit=new Button{Content="Edit"};var delete=new Button{Content="Delete"};
            edit.Click+=async(_,_)=>{if(IsCurrentSession(c,epoch))await GuardAsync(()=>EditMemoryAsync(entry));};delete.Click+=async(_,_)=>{if(IsCurrentSession(c,epoch))await GuardAsync(async()=>{if(await AskAsync("Delete memory?",Text(entry.Text("content")),"Delete")==ContentDialogResult.Primary&&IsCurrentSession(c,epoch)){await c.DeleteAsync("/api/memories/"+Wire.Segment(entry.Text("id")),entry.Text("updated_at"),accountLifetime.Token);if(IsCurrentSession(c,epoch))await RefreshMemoryAsync();}});};actions.Children.Add(edit);actions.Children.Add(delete);row.Children.Add(actions);panel.Children.Add(row);
        }
        if(panel.Children.Count==0)panel.Children.Add(Text("No saved memory."));
    }
    private async Task EditMemoryAsync(JsonNode? entry)
    {
        accountView.Require(SettingsSection.Memory);var c=Need();long epoch=accountEpoch;var category=new ComboBox{Header="Category",HorizontalAlignment=HorizontalAlignment.Stretch};foreach(string value in new[]{"profile","preference","context"})category.Items.Add(value);category.SelectedItem=entry.Text("category","preference");
        var content=new TextBox{Header="Memory",Text=entry.Text("content"),AcceptsReturn=true,TextWrapping=TextWrapping.Wrap,MinHeight=100};var form=new StackPanel{Spacing=12};form.Children.Add(category);form.Children.Add(content);
        if(await AskAsync(entry is null?"Add memory":"Edit memory",form,"Save")!=ContentDialogResult.Primary||!IsCurrentSession(c,epoch))return;
        var payload=new{category=category.SelectedItem as string??"preference",content=content.Text};
        if(entry is null)await c.PostAsync("/api/memories",payload,accountLifetime.Token);else await c.PutAsync("/api/memories/"+Wire.Segment(entry.Text("id")),payload,accountLifetime.Token);if(IsCurrentSession(c,epoch))await RefreshMemoryAsync();
    }
    private async Task ExportMemoryAsync()
    {
        var c=Need();long epoch=accountEpoch;var result=await c.GetAsync("/api/memories/export",accountLifetime.Token);if(!IsCurrentSession(c,epoch))return;
        var picker=new Windows.Storage.Pickers.FileSavePicker{SuggestedFileName="Agent-memory"};picker.FileTypeChoices.Add("JSON",new List<string>{".json"});WinRT.Interop.InitializeWithWindow.Initialize(picker,WinRT.Interop.WindowNative.GetWindowHandle(this));var file=await picker.PickSaveFileAsync();if(file is not null&&IsCurrentSession(c,epoch)){await Windows.Storage.FileIO.WriteTextAsync(file,result.ToJsonString());Notify("Memory exported to the selected file.",false);}
    }
    private async Task ResetPasswordAsync()
    {
        accountView.Require(SettingsSection.Account);var c=Need();long epoch=accountEpoch;string current=C<PasswordBox>("CurrentPassword").Password,next=C<PasswordBox>("NewPassword").Password,confirm=C<PasswordBox>("ConfirmPassword").Password;
        C<PasswordBox>("CurrentPassword").Password=C<PasswordBox>("NewPassword").Password=C<PasswordBox>("ConfirmPassword").Password="";
        if(next!=confirm||next.EnumerateRunes().Count()<12||Encoding.UTF8.GetByteCount(next)>72)throw new ArgumentException("New passwords must match and contain at least 12 characters and at most 72 UTF-8 bytes.");
        await c.PostAsync("/api/auth/password",new{current_password=current,new_password=next},accountLifetime.Token);if(!IsCurrentSession(c,epoch))return;await c.GetAsync("/api/auth/me",accountLifetime.Token);Notify("Password updated. Other Agent sessions were signed out.",false);
    }
    private async Task SignOutAsync()
    {
        var c=Need();if(await AskAsync("Sign out on this device?",Text("Saved conversations remain on your Agent server."),"Sign out")!=ContentDialogResult.Primary)return;
        await c.SignOutAsync(accountLifetime.Token);ClearAccount();
    }
    private async Task ChangeServerAsync()
    {
        if(await AskAsync("Change Agent server?",Text("This clears the current local session and account-scoped views. It does not delete server conversations."),"Change server")!=ContentDialogResult.Primary)return;
        if(device is not null)await CancelDeviceAsync();if(client is not null){await client.SignOutAsync(accountLifetime.Token);}ClearAccount();C<TextBox>("Endpoint").Focus(FocusState.Programmatic);
    }
    private void RenderUpdates(WindowsUpdateStatus status)
    {
        if (closed) return;
        C<TextBlock>("UpdateTitle").Text = status.Phase switch
        {
            WindowsUpdatePhase.Idle => "Not checked yet",
            WindowsUpdatePhase.Checking => "Checking for updates",
            WindowsUpdatePhase.Current => "Up to date",
            WindowsUpdatePhase.Available => "Update available",
            WindowsUpdatePhase.Downloading => "Downloading update",
            WindowsUpdatePhase.Verifying => "Verifying update",
            WindowsUpdatePhase.Ready => "Ready to install",
            WindowsUpdatePhase.Restarting => "Restarting",
            WindowsUpdatePhase.Error => "Update failed",
            _ => "Updates unavailable"
        };
        SetSettingsText("OfferedVersion", status.Version is { } version ? $"Agent {version}" : "");
        SetSettingsText("UpdateStatus", status.Phase switch
        {
            WindowsUpdatePhase.Available => "",
            WindowsUpdatePhase.Idle or WindowsUpdatePhase.Checking or WindowsUpdatePhase.Current => "",
            WindowsUpdatePhase.Downloading => $"{status.Progress}%",
            _ => status.Message
        });

        bool available = status.Phase == WindowsUpdatePhase.Available;
        bool ready = status.Phase == WindowsUpdatePhase.Ready;
        bool indeterminate = status.Phase is WindowsUpdatePhase.Checking or WindowsUpdatePhase.Verifying or WindowsUpdatePhase.Restarting;
        bool working = indeterminate || status.Phase == WindowsUpdatePhase.Downloading;
        C<StackPanel>("UpdateActions").Visibility = available || ready ? Visibility.Visible : Visibility.Collapsed;
        C<Button>("UpdatePrimary").Content = ready ? "Restart and install" : "Download update";
        C<Button>("CheckUpdates").IsEnabled = !working && !ready && !status.RetryingNotes && status.Phase != WindowsUpdatePhase.NotInstalled;
        C<Button>("UpdatePrimary").IsEnabled = !status.RetryingNotes;
        var progress = C<ProgressBar>("UpdateProgress");
        progress.Visibility = working || ready ? Visibility.Visible : Visibility.Collapsed;
        progress.IsIndeterminate = indeterminate;
        progress.Value = status.Progress;

        var disclosure = C<Expander>("WhatsNew");
        disclosure.Visibility = status.Version is null ? Visibility.Collapsed : Visibility.Visible;
        C<Button>("RetryChangelog").Visibility = available && status.Notes is null ? Visibility.Visible : Visibility.Collapsed;
        C<Button>("RetryChangelog").IsEnabled = !status.RetryingNotes;
        C<TextBlock>("ChangelogUnavailable").Text = status.RetryingNotes ? "Checking changelog…" : "Changelog unavailable.";
        if (renderedOffer != status.Version || !ReferenceEquals(renderedNotes, status.Notes))
        {
            if (renderedOffer != status.Version) disclosure.IsExpanded = false;
            renderedOffer = status.Version;
            renderedNotes = status.Notes;
            RenderUpdateNotes(disclosure, status.Notes);
        }
    }

    private void RenderUpdateNotes(Expander disclosure, ReleaseNotes? notes)
    {
        var notesContent = C<StackPanel>("Changelog");
        notesContent.Children.Clear();
        C<TextBlock>("ChangelogUnavailable").Visibility = notes is null ? Visibility.Visible : Visibility.Collapsed;
        disclosure.Header = "What’s new";
        if (notes is null) return;

        int count = notes.Improvements.Count + notes.Fixes.Count;
        disclosure.Header = $"What’s new · {count} {(count == 1 ? "change" : "changes")}";
        foreach (var (heading, bullets) in new[] { ("Improvements", notes.Improvements), ("Fixes", notes.Fixes) })
        {
            if (bullets.Count == 0) continue;
            notesContent.Children.Add(new TextBlock { Text = heading, FontWeight = Microsoft.UI.Text.FontWeights.SemiBold });
            foreach (string bullet in bullets) notesContent.Children.Add(Text($"• {bullet}"));
        }
        if (count == 0) notesContent.Children.Add(Text("No user-facing Windows changes in this source range."));

        var details = new StackPanel { Spacing = 8 };
        details.Children.Add(CommitLink($"{notes.From[..7]}…{notes.To[..7]}",
            $"{WindowsGithubSource.Repository}/compare/{notes.From}...{notes.To}",
            $"Compare source range {notes.From[..7]} to {notes.To[..7]} on GitHub (opens in your browser)"));
        foreach (var commit in notes.Commits)
        {
            var row = new Grid { ColumnSpacing = 9 };
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = GridLength.Auto });
            row.ColumnDefinitions.Add(new ColumnDefinition { Width = new GridLength(1, GridUnitType.Star) });
            row.Children.Add(CommitLink(commit.Sha[..7], $"{WindowsGithubSource.Repository}/commit/{commit.Sha}",
                $"View commit {commit.Sha[..7]} on GitHub (opens in your browser)"));
            var subject = Text(commit.Subject);
            subject.VerticalAlignment = VerticalAlignment.Center;
            Grid.SetColumn(subject, 1);
            row.Children.Add(subject);
            details.Children.Add(row);
        }
        notesContent.Children.Add(new Expander { Header = "Commit details", Content = details,
            HorizontalAlignment = HorizontalAlignment.Stretch, HorizontalContentAlignment = HorizontalAlignment.Stretch });
    }

    private HyperlinkButton CommitLink(string caption, string uri, string accessibleName)
    {
        // The native control opens its URI only on activation, never on disclosure.
        var link = new HyperlinkButton
        {
            Content = caption,
            NavigateUri = new Uri(uri),
            Style = (Style)C<StackPanel>("Changelog").Resources["CommitLinkStyle"]
        };
        AutomationProperties.SetName(link, accessibleName);
        ToolTipService.SetToolTip(link, accessibleName);
        return link;
    }
}
