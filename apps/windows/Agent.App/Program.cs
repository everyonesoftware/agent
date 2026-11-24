using Microsoft.UI.Dispatching;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Velopack;
namespace Agent.App;
internal static class Program
{
    [STAThread] public static void Main()
    {
        VelopackApp.Build().SetAutoApplyOnStartup(false).Run();
        WinRT.ComWrappersSupport.InitializeComWrappers();
        Application.Start(callback=>{SynchronizationContext.SetSynchronizationContext(new DispatcherQueueSynchronizationContext(DispatcherQueue.GetForCurrentThread()));_ = new AgentApplication();});
    }
}
public sealed partial class AgentApplication:Application,Microsoft.UI.Xaml.Markup.IXamlMetadataProvider
{
    private readonly Microsoft.UI.Xaml.XamlTypeInfo.XamlControlsXamlMetaDataProvider metadata=new();
    public Microsoft.UI.Xaml.Markup.IXamlType GetXamlType(Type type)=>metadata.GetXamlType(type);
    public Microsoft.UI.Xaml.Markup.IXamlType GetXamlType(string name)=>metadata.GetXamlType(name);
    public Microsoft.UI.Xaml.Markup.XmlnsDefinition[] GetXmlnsDefinitions()=>metadata.GetXmlnsDefinitions();
    private MainWindow? window;
    public AgentApplication(){UnhandledException+=(_,e)=>StartupDiagnostics.Write(e.Exception);}
    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        Resources.MergedDictionaries.Add(new XamlControlsResources());Resources["SystemAccentColor"]=Windows.UI.Color.FromArgb(255,180,214,163);
        window=new MainWindow();RunningTaskbarIdentity.Attach(window);window.Activate();
        _=window.CheckOnStartupAsync();
    }
}
internal static class StartupDiagnostics
{
    public static void Write(Exception exception)
    {
        string? directory=Environment.GetEnvironmentVariable("AGENT_STARTUP_DIAGNOSTICS");if(string.IsNullOrEmpty(directory))return;
        try{Directory.CreateDirectory(directory);File.WriteAllText(Path.Combine(directory,"startup-error.txt"),$"{exception.GetType().Name}\nHRESULT 0x{exception.HResult:X8}\n{exception.StackTrace}");}catch{/* Diagnostics must not replace startup failure. */}
    }
}
