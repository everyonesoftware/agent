using System.Reflection;
using System.Text.Json;
using System.Text.Json.Nodes;
using Agent.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Markup;
using Microsoft.UI.Xaml.Media;
using Microsoft.UI.Xaml.Media.Imaging;
using Windows.UI;
namespace Agent.App;

public sealed partial class MainWindow:Window
{
    private readonly Grid root;
    private readonly SessionVault vault=new();
    private AgentClient? client;
    private CancellationTokenSource accountLifetime=new();
    private CancellationTokenSource? request;
    private readonly ChatSession chat=new();
    private readonly AccountViewState accountView=new();
    private bool busy,rendering,restoring=true,closed;
    private long accountEpoch;
    private string activeProfile="",connectionId="",researchTask="";
    private JsonNode? usage,researchSettings,device;
    private ContentDialog? currentDialog;
    private readonly WindowsUpdateController updates;
    private readonly ChangelogClient changelog=new();
    private string? notesVersion;
    private static readonly SolidColorBrush Sage=new(Color.FromArgb(255,180,214,163));
    private T C<T>(string name) where T:class => (T)root.FindName(name);
    private static TextBlock Text(string value,double size=14)=>new(){Text=value,FontSize=size,TextWrapping=TextWrapping.Wrap,IsTextSelectionEnabled=true};
    private AgentClient Need()=>client??throw new AgentException("Sign in to your Agent server first.");
    private bool IsCurrentSession(AgentClient captured,long epoch)=>!closed&&client==captured&&epoch==accountEpoch;
    public MainWindow()
    {
        using var stream=Assembly.GetExecutingAssembly().GetManifestResourceStream("Agent.App.Shell.xaml")!;using var reader=new StreamReader(stream);
        root=(Grid)XamlReader.Load(reader.ReadToEnd());Content=root;Title="Agent";
        AppWindow.SetIcon(Path.Combine(AppContext.BaseDirectory,"Assets","Agent.ico"));
        var brandImage=new BitmapImage(new Uri(Path.Combine(AppContext.BaseDirectory,"Assets","Agent.png")));
        foreach(string name in new[]{"AppIcon","SignInIcon","SidebarIcon"})C<Image>(name).Source=brandImage;
        ExtendsContentIntoTitleBar=true;SetTitleBar(C<Border>("TitleBar"));AppWindow.Resize(new Windows.Graphics.SizeInt32(1240,850));
        AppWindow.TitleBar.ButtonBackgroundColor=Color.FromArgb(255,24,27,26);AppWindow.TitleBar.ButtonForegroundColor=Color.FromArgb(255,225,230,226);
        AppWindow.TitleBar.ButtonInactiveBackgroundColor=Color.FromArgb(255,24,27,26);AppWindow.TitleBar.ButtonInactiveForegroundColor=Color.FromArgb(255,163,174,166);
        updates=new(new WindowsUpdateBackend(),PreserveSessionAsync);updates.Changed+=status=>DispatcherQueue.TryEnqueue(()=>RenderUpdates(status));
        string installed=Assembly.GetExecutingAssembly().GetCustomAttribute<AssemblyInformationalVersionAttribute>()?.InformationalVersion.Split('+')[0]??"Unknown";
        C<TextBlock>("InstalledVersion").Text="Current Version: "+installed;
        C<TextBlock>("SidebarVersion").Text="Version "+installed;
        Hook("Login",()=>LoginAsync(false));Hook("Register",()=>LoginAsync(true));
        Hook("NewChat",()=>{chat.Reset();C<TextBox>("Composer").Text="";C<CheckBox>("ResearchToggle").IsChecked=false;RenderMessages();UpdateActiveConversation();ShowSettings(false);return Task.CompletedTask;});
        Hook("SettingsNav", async () =>
        {
            if (busy) return;
            if (settingsOpen) { await LeaveSettingsAsync(); return; }
            NavigateSettings(true);
            openingSettings = LoadOpeningSettingsAsync();
            await openingSettings;
        }, false);
        Hook("SettingsBack", LeaveSettingsAsync, false);
        Hook("Send",SendAsync);Hook("CancelRequest",()=>{request?.Cancel();return Task.CompletedTask;},false);
        Hook("StopResearch",()=>EndResearchAsync("stop"),false);Hook("RevokeResearch",()=>EndResearchAsync("revoke"),false);
        WireChatLayout();
        WireSettingsLayout();
        Hook("RefreshConnection",()=>RefreshProviderAsync());Hook("RefreshAllowance",()=>RefreshUsageAsync(true));
        Hook("ConnectChatGPT",()=>StartDeviceAsync("/auth/start"));Hook("AddAccount",()=>StartDeviceAsync("/auth/start?mode=add"));
        Hook("DisconnectAccount",()=>ChangeProfileAsync(activeProfile,true));
        C<ComboBox>("Accounts").SelectionChanged+=async(_,_)=>{if(rendering||busy)return;if(C<ComboBox>("Accounts").SelectedItem is ComboBoxItem item&&item.Tag is JsonNode p){string id=p.Text("id");if(id==activeProfile)return;await GuardAsync(()=>p.Flag("registration_pending")?StartDeviceAsync("/auth/start?mode=retry&profile_id="+Wire.Segment(id)):ChangeProfileAsync(id,false));}};
        Hook("VerifyDevice",async()=>{if(device is not null)await Windows.System.Launcher.LaunchUriAsync(ClientPresentation.VerificationUri(device.Text("verification_url")));});
        Hook("PollDevice",PollDeviceAsync);Hook("CancelDevice",CancelDeviceAsync);
        Hook("UsageHistory",()=>ShowUsageHistoryAsync(false));Hook("ResearchHistory",()=>ShowUsageHistoryAsync(true));
        Hook("SaveBraveKey",()=>ChangeResearchAsync("verify"));Hook("CheckBraveKey",()=>ChangeResearchAsync("check"));Hook("RemoveBraveKey",()=>ChangeResearchAsync("remove"));Hook("SaveBudget",SaveBudgetAsync);
        C<ToggleSwitch>("MemoryEnabled").Toggled+=async(_,_)=>{if(!rendering&&!busy)await GuardAsync(async()=>{accountView.Require(SettingsSection.Memory);await Need().PutAsync("/api/memory-settings",new{enabled=C<ToggleSwitch>("MemoryEnabled").IsOn},accountLifetime.Token);await RefreshMemoryAsync();});};
        Hook("AddMemory",()=>EditMemoryAsync(null));Hook("ExportMemory",ExportMemoryAsync);Hook("ResetPassword",ResetPasswordAsync);
        Hook("SignOut",SignOutAsync);Hook("ChangeServer",ChangeServerAsync);Hook("TestConnection",async()=>{await Need().GetAsync("/api/auth/me",accountLifetime.Token);Notify("Connected to your Agent server.",false);});
        Hook("CheckUpdates",updates.CheckAsync,false);Hook("UpdatePrimary",async()=>{if(updates.Status.Phase==WindowsUpdatePhase.Ready)await updates.RestartAsync();else await updates.DownloadAsync();},false);
        C<Expander>("WhatsNew").Expanding+=async(_,_)=>await LoadNotesAsync();
        Closed+=(_,_)=>{closed=true;accountLifetime.Cancel();request?.Cancel();client?.Dispose();updates.Dispose();changelog.Dispose();};
        root.Loaded+=async(_,_)=>await GuardAsync(RestoreAsync);
    }
    private void Hook(string name,Func<Task> action,bool serialize=true)=>C<Button>(name).Click+=async(_,_)=>await GuardAsync(action,serialize);
    private async Task GuardAsync(Func<Task> action,bool serialize=true)
    {
        if(closed||(serialize&&busy))return;
        if(serialize){busy=true;SetBusy();}
        long epoch=accountEpoch;
        try{await action();}
        catch(OperationCanceledException){if(epoch==accountEpoch&&!closed)Notify("Request cancelled. Check saved history before resending a message.",false);}
        catch(SessionExpiredException){if(epoch==accountEpoch)ClearAccount();}
        catch(Exception e){if(epoch==accountEpoch&&!closed)Notify(e is AgentException or ArgumentException?e.Message:"The operation failed. Check your connection and try again.");}
        finally{if(serialize){busy=false;SetBusy();}}
    }
    private void SetBusy()
    {
        C<Button>("SettingsBack").IsEnabled = !busy || openingSettingsCancellation is { IsCancellationRequested: false };
        foreach(string name in new[]{"Login","Register","NewChat","Send","SettingsNav","RefreshConnection","RefreshAllowance","ConnectChatGPT","AddAccount","DisconnectAccount","PollDevice","CancelDevice","SaveBraveKey","CheckBraveKey","RemoveBraveKey","SaveBudget","AddMemory","ExportMemory","ResetPassword","SignOut","ChangeServer","TestConnection"})C<Button>(name).IsEnabled=!busy;
        foreach(string name in new[]{"ConnectChatGPT","AddAccount","DisconnectAccount"})C<Button>(name).IsEnabled=!busy&&accountView.IsLoaded(SettingsSection.Provider);
        foreach(string name in new[]{"SaveBraveKey","CheckBraveKey","RemoveBraveKey","SaveBudget"})C<Button>(name).IsEnabled=!busy&&accountView.IsLoaded(SettingsSection.Research);
        foreach(string name in new[]{"AddMemory","ExportMemory"})C<Button>(name).IsEnabled=!busy&&accountView.IsLoaded(SettingsSection.Memory);
        C<Button>("ResetPassword").IsEnabled=!busy&&accountView.IsLoaded(SettingsSection.Account);
        C<ListView>("History").IsEnabled=!busy;C<ComboBox>("Accounts").IsEnabled=!busy&&accountView.IsLoaded(SettingsSection.Provider);C<TextBox>("Composer").IsEnabled=!busy;C<ComboBox>("Models").IsEnabled=!busy;C<CheckBox>("ResearchToggle").IsEnabled=!busy;C<ToggleSwitch>("MemoryEnabled").IsEnabled=!busy&&accountView.IsLoaded(SettingsSection.Memory);
        C<Grid>("RequestActions").Visibility=C<Button>("CancelRequest").Visibility=request is null?Visibility.Collapsed:Visibility.Visible;
    }
    private void Notify(string message,bool error=true){C<InfoBar>("Notice").Message=message;C<InfoBar>("Notice").Severity=error?InfoBarSeverity.Error:InfoBarSeverity.Informational;C<InfoBar>("Notice").IsOpen=true;}
    private void RenderAuth(bool signedIn)
    {
        C<StackPanel>("Loading").Visibility=restoring?Visibility.Visible:Visibility.Collapsed;
        C<ScrollViewer>("SignIn").Visibility=!restoring&&!signedIn?Visibility.Visible:Visibility.Collapsed;C<Grid>("Workspace").Visibility=!restoring&&signedIn?Visibility.Visible:Visibility.Collapsed;
    }
    private void ResetAccountView()
    {
        accountEpoch++;accountLifetime.Cancel();accountLifetime.Dispose();accountLifetime=new();request?.Cancel();
        currentDialog?.Hide();currentDialog=null;chat.Reset();device=usage=researchSettings=null;activeProfile=connectionId=researchTask="";
        rendering=true;
        try
        {
            accountView.Reset(name=>C<PasswordBox>(name).Password="",()=>
            {
                foreach(string name in new[]{"Composer","HistorySearch","BudgetTimezone","Username"})C<TextBox>(name).Text="";
                foreach(string name in new[]{"SignedInAs","AccountInitial","ServerDescription","ResearchStatus","DeviceCode","LoginFeedback"})C<TextBlock>(name).Text="";
                foreach(string name in new[]{"ProviderMessage","AllowanceFreshness","AllowanceMessage"})SetSettingsText(name, "");
                savedConversations=[];historyLoaded=false;RenderConversationHistory();C<ComboBox>("Models").Items.Clear();C<ComboBox>("Accounts").Items.Clear();
                foreach(string name in new[]{"Messages","MemoryEntries","AllowanceWindows"})C<StackPanel>(name).Children.Clear();
                C<StackPanel>("AllowanceWindows").Visibility=Visibility.Collapsed;
                C<ComboBox>("Accounts").Visibility=Visibility.Collapsed;
                C<CheckBox>("ResearchToggle").IsChecked=false;
                foreach(string name in new[]{"BudgetEnabled","MemoryEnabled"})C<ToggleSwitch>(name).IsOn=false;
                C<NumberBox>("BudgetMaximum").Value=double.NaN;C<ComboBox>("BudgetPeriod").SelectedIndex=-1;
                C<StackPanel>("DeviceLogin").Visibility=Visibility.Collapsed;
                foreach(string name in new[]{"StopResearch","RevokeResearch","CancelRequest","AddAccount","DisconnectAccount"})C<Button>(name).Visibility=Visibility.Collapsed;
                C<Button>("ConnectChatGPT").Visibility=Visibility.Visible;SetConnectionBadge("Unavailable");
                C<InfoBar>("Notice").IsOpen=false;C<InfoBar>("Notice").Message="";
                ShowSettings(false);C<ScrollViewer>("SettingsView").ChangeView(null,0,null);RenderMessages();
            });
        }
        finally{rendering=false;SetBusy();}
    }
    private void ActivateClient(Uri endpoint)
    {
        ResetAccountView();client?.Dispose();
        var activated=new AgentClient(endpoint,vault);client=activated;
        activated.SessionExpired+=()=>DispatcherQueue.TryEnqueue(()=>{if(client==activated)ClearAccount();});
    }
    private void ClearAccount()
    {
        ResetAccountView();client?.Dispose();client=null;restoring=false;RenderAuth(false);
    }
    private async Task RestoreAsync()
    {
        try
        {
            string? endpoint=LocalSettings.ReadEndpoint();C<TextBox>("Endpoint").Text=endpoint??"";
            if(endpoint is null){restoring=false;RenderAuth(false);return;}
            ActivateClient(ServerEndpoint.Parse(endpoint));var c=Need();long epoch=accountEpoch;var user=await c.RestoreAsync(accountLifetime.Token);if(!IsCurrentSession(c,epoch))return;
            restoring=false;if(user is null){RenderAuth(false);return;}RenderAuth(true);RenderAccountName(user.Text("username"));await RefreshWorkspaceAsync();
        }
        catch(SessionExpiredException){ClearAccount();}
        catch{restoring=false;RenderAuth(false);Notify("Your saved server could not be reached. Retry sign-in or change the server; your saved session has not been removed.");}
    }
    private async Task LoginAsync(bool register)
    {
        var endpoint=ServerEndpoint.Parse(C<TextBox>("Endpoint").Text);string username=C<TextBox>("Username").Text,password=C<PasswordBox>("Password").Password;C<PasswordBox>("Password").Password="";
        ActivateClient(endpoint);var c=Need();long epoch=accountEpoch;await c.SignInAsync(username,password,register,accountLifetime.Token);if(!IsCurrentSession(c,epoch))return;
        LocalSettings.SaveEndpoint(endpoint);var user=await c.GetAsync("/api/auth/me",accountLifetime.Token);if(!IsCurrentSession(c,epoch))return;restoring=false;RenderAuth(true);RenderAccountName(user.Text("username"));await RefreshWorkspaceAsync();
    }
    private async Task RefreshWorkspaceAsync(){await RefreshModelsAsync();await RefreshHistoryAsync();}
    private async Task RefreshModelsAsync(CancellationToken? cancellation = null)
    {
        var c=Need();long epoch=accountEpoch;var result=await c.GetAsync("/api/models",cancellation ?? accountLifetime.Token);cancellation?.ThrowIfCancellationRequested();if(!IsCurrentSession(c,epoch))return;
        C<ComboBox>("Models").Items.Clear();foreach(var m in result.Items("models"))C<ComboBox>("Models").Items.Add(new ComboBoxItem{Content=m.Text("name",m.Text("id")),Tag=m.Text("id")});if(C<ComboBox>("Models").Items.Count>0)C<ComboBox>("Models").SelectedIndex=0;
    }
    private async Task RefreshHistoryAsync()
    {
        var c=Need();long epoch=accountEpoch;var result=await c.GetAsync("/api/conversations",accountLifetime.Token);if(!IsCurrentSession(c,epoch))return;
        savedConversations=ConversationHistory.Read(result);historyLoaded=true;
        if(savedConversations.FirstOrDefault(item=>item.Id==chat.ConversationId) is {} current)chatTitle=current.Title;
        RenderConversationTitle();RenderConversationHistory();
    }
    private async Task LoadConversationAsync(string id)
    {
        var c=Need();long epoch=accountEpoch;var result=await c.GetAsync("/api/conversations/"+Wire.Segment(id),accountLifetime.Token);if(!IsCurrentSession(c,epoch))return;
        chat.Load(id,result);chatTitle=result["conversation"].Text("title","Conversation");C<CheckBox>("ResearchToggle").IsChecked=false;RenderMessages(result.Items("messages").ToArray());UpdateActiveConversation();ShowSettings(false);
    }
    private void RenderMessages(JsonNode[]? stored=null)
    {
        var panel=C<StackPanel>("Messages");panel.Children.Clear();
        bool empty=chat.Messages.Count==0;
        C<StackPanel>("Welcome").Visibility=empty?Visibility.Visible:Visibility.Collapsed;
        panel.Visibility=empty?Visibility.Collapsed:Visibility.Visible;
        if(chat.ConversationId is null)chatTitle=NewConversationTitle;
        RenderConversationTitle();
        if(empty){C<ScrollViewer>("MessageScroll").ChangeView(null,0,null);return;}
        for(int i=0;i<chat.Messages.Count;i++)
        {
            var message=chat.Messages[i];bool isUser=message.Role=="user";
            var stack=new StackPanel{Spacing=8,HorizontalAlignment=isUser?HorizontalAlignment.Right:HorizontalAlignment.Stretch};
            var sender=new TextBlock{Text=isUser?"You":"Agent",FontSize=11,Foreground=isUser?Muted:null,HorizontalAlignment=stack.HorizontalAlignment};
            if(!isUser)sender.ClearValue(TextBlock.ForegroundProperty);
            stack.Children.Add(sender);
            var body=Text(message.Content);body.LineHeight=24;
            if(isUser)stack.Children.Add(new Border{Background=SelectedConversation,CornerRadius=new CornerRadius(12,12,3,12),Padding=new Thickness(16,10,16,10),Child=body});
            else stack.Children.Add(body);
            string run=stored is not null&&i<stored.Length?stored[i].Text("run_id"):"";if(run!="")AddInspector(stack,run);
            panel.Children.Add(new Border{Margin=isUser?new Thickness(36,0,0,0):new Thickness(0),Child=stack});
        }
        C<ScrollViewer>("MessageScroll").ChangeView(null,double.MaxValue,null);
    }
    private void AddInspector(StackPanel stack,string run)
    {
        long epoch=accountEpoch;var button=new Button{Content="Run Inspector"};button.Click+=async(_,_)=>{if(epoch==accountEpoch)await GuardAsync(async()=>{var c=Need();var result=await c.GetAsync("/api/runs/"+Wire.Segment(run),accountLifetime.Token);if(IsCurrentSession(c,epoch))await ShowJsonAsync("Run Inspector",result);});};stack.Children.Add(button);
    }
    private async Task SendAsync()
    {
        string content=C<TextBox>("Composer").Text;if(string.IsNullOrWhiteSpace(content))return;
        string model=(C<ComboBox>("Models").SelectedItem as ComboBoxItem)?.Tag as string??"";
        var c=Need();long epoch=accountEpoch,revision=chat.Revision;chat.Messages.Add(new("user",content));ChatRequest payload;
        try{payload=chat.CreateRequest(model);}catch{chat.Messages.RemoveAt(chat.Messages.Count-1);throw;}
        request=CancellationTokenSource.CreateLinkedTokenSource(accountLifetime.Token);SetBusy();
        try
        {
            if(C<CheckBox>("ResearchToggle").IsChecked==true)
            {
                var capabilities=await c.GetAsync("/api/research",request.Token);if(!IsCurrentSession(c,epoch))return;
                var created=await c.PostAsync("/api/research/tasks",payload,request.Token);if(!IsCurrentSession(c,epoch))return;
                researchTask=created["task"].Text("id");if(researchTask==""||created["task"].Text("scope")!="public_web"||created["task"].Text("status")!="pending")throw new AgentException("Research consent could not be prepared.");
                var choice=await AskAsync("Allow Web Research for this task?",Text(capabilities.Text("message")+"\n\nOnly this exact task is approved. No private resources, files, signed-in sites or publishing.\n\n"+content),"Allow research","Answer without browsing");
                if(!IsCurrentSession(c,epoch))return;
                if(choice==ContentDialogResult.Primary){var approved=await c.PostAsync("/api/research/tasks/"+Wire.Segment(researchTask)+"/approve",new{},request.Token);if(approved["task"].Text("status")!="approved")throw new AgentException("Research approval was not confirmed.");payload=payload.WithResearch(researchTask);C<Button>("StopResearch").Visibility=C<Button>("RevokeResearch").Visibility=Visibility.Visible;}
                else{await c.PostAsync("/api/research/tasks/"+Wire.Segment(researchTask)+"/revoke",new{},request.Token);researchTask="";if(choice!=ContentDialogResult.Secondary){chat.Messages.RemoveAt(chat.Messages.Count-1);return;}}
            }
            C<TextBox>("Composer").Text="";RenderMessages();
            var reply=await c.PostAsync("/api/chat",payload,request.Token);if(!IsCurrentSession(c,epoch)||!chat.AcceptReply(revision,reply))return;RenderMessages();
            if(C<StackPanel>("Messages").Children.LastOrDefault() is Border b&&b.Child is StackPanel s&&reply.Text("run_id")!="")AddInspector(s,reply.Text("run_id"));
            await RefreshHistoryAsync();
        }
        catch{if(IsCurrentSession(c,epoch))Notify("The message was not confirmed. Check saved history before resending; no automatic retry was sent.");throw;}
        finally{request.Dispose();request=null;researchTask="";C<Button>("StopResearch").Visibility=C<Button>("RevokeResearch").Visibility=Visibility.Collapsed;SetBusy();}
    }
    private async Task EndResearchAsync(string action)
    {
        if(researchTask=="")return;var c=Need();long epoch=accountEpoch;string id=researchTask;
        var result=await c.PostAsync("/api/research/tasks/"+Wire.Segment(id)+"/"+action,new{},accountLifetime.Token);if(!IsCurrentSession(c,epoch)||id!=researchTask)return;
        if(result["task"].Text("status")!=(action=="revoke"?"revoked":"cancelled"))throw new AgentException("The server did not confirm the research control. Retry.");request?.Cancel();Notify(action=="revoke"?"Research permission revoked.":"Research stopped.",false);
    }
    private async Task<ContentDialogResult> AskAsync(string title,UIElement content,string primary="",string secondary="")
    {
        var dialog=new ContentDialog{Title=title,Content=content,CloseButtonText="Cancel",PrimaryButtonText=primary,SecondaryButtonText=secondary,XamlRoot=root.XamlRoot,RequestedTheme=ElementTheme.Dark};currentDialog=dialog;try{return await dialog.ShowAsync();}finally{if(currentDialog==dialog)currentDialog=null;}
    }
    private async Task ShowJsonAsync(string title,JsonNode result)
    {
        await AskAsync(title,new ScrollViewer{MaxHeight=540,Content=Text(result.ToJsonString(new JsonSerializerOptions{WriteIndented=true}))});
    }
    private async Task PreserveSessionAsync()
    {
        if(busy||request is not null||currentDialog is not null)throw new AgentException("Finish the current operation before restarting.");
        if(!string.IsNullOrWhiteSpace(C<TextBox>("Composer").Text))throw new AgentException("Send or clear the unsent draft before restarting.");
        if(client is not null){string? saved=await vault.ReadAsync(client.Endpoint.AbsoluteUri);if(string.IsNullOrEmpty(saved))throw new AgentException("The session could not be saved.");LocalSettings.SaveEndpoint(client.Endpoint);}
    }
}
