using System.Diagnostics;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using Microsoft.UI.Xaml;
using Velopack.Locators;

namespace Agent.App;

// Window-only metadata: keep Velopack's process/shortcut identity and update behavior unchanged.
internal sealed class RunningTaskbarIdentity:IDisposable
{
    private static readonly PropertyKey RelaunchCommand=new(2);
    private static readonly PropertyKey RelaunchIconResource=new(3);
    private static readonly PropertyKey RelaunchDisplayNameResource=new(4);
    private static readonly PropertyKey AppUserModelId=new(5);
    private IPropertyStore? store;

    private RunningTaskbarIdentity(IPropertyStore store)=>this.store=store;

    public static void Attach(Window window)
    {
        RunningTaskbarIdentity? identity=null;
        try
        {
            var locator=VelopackLocator.Current;
            // Development/portable launches must not invent an installed identity or relaunch target.
            if(locator.CurrentlyInstalledVersion is null||string.IsNullOrWhiteSpace(locator.AppUserModelId)||string.IsNullOrWhiteSpace(locator.RootAppDir))return;
            string launcher=Path.Combine(locator.RootAppDir,"Agent.exe");
            using var original=File.OpenRead(Path.Combine(AppContext.BaseDirectory,"Assets","Agent.ico"));
            string digest=Convert.ToHexStringLower(SHA256.HashData(original));
            string iconPath=Path.Combine(AppContext.BaseDirectory,"Assets",$"Agent-{digest}.ico");
            if(!File.Exists(iconPath)||!File.Exists(launcher))throw new FileNotFoundException("Installed taskbar icon or stable Agent launcher is missing.");
            Guid iid=typeof(IPropertyStore).GUID;
            Marshal.ThrowExceptionForHR(SHGetPropertyStoreForWindow(WinRT.Interop.WindowNative.GetWindowHandle(window),ref iid,out var propertyStore));
            identity=new(propertyStore);
            // Shell reads relaunch metadata when the explicit HWND ID is assigned; publish ID last.
            identity.SetString(RelaunchCommand,$"\"{launcher}\"");
            identity.SetString(RelaunchIconResource,iconPath+",0");
            identity.SetString(RelaunchDisplayNameResource,"Agent");
            identity.SetString(AppUserModelId,locator.AppUserModelId);
            // VT_EMPTY must reach the live HWND, not Window.Closed after destruction.
            var attached=identity;
            window.AppWindow.Closing+=(_,_)=>attached.Dispose();
        }
        catch(Exception exception)
        {
            identity?.Dispose();
            RecordFailure(exception);
        }
    }

    private void SetString(PropertyKey key,string text)
    {
        var value=new PropVariant{VarType=31,Pointer=Marshal.StringToCoTaskMemUni(text)}; // VT_LPWSTR
        try{Marshal.ThrowExceptionForHR(store!.SetValue(ref key,ref value));}
        finally{Marshal.FreeCoTaskMem(value.Pointer);}
    }

    public void Dispose()
    {
        var releasing=store;if(releasing is null)return;store=null;
        try
        {
            // Attempt every removal even if an earlier property failed; SetValue is immediate (no Commit).
            foreach(var property in new[]{AppUserModelId,RelaunchCommand,RelaunchIconResource,RelaunchDisplayNameResource})
            {
                var key=property;PropVariant empty=default; // VT_EMPTY
                try{Marshal.ThrowExceptionForHR(releasing.SetValue(ref key,ref empty));}
                catch(Exception exception){RecordFailure(exception);}
            }
        }
        finally
        {
            try{Marshal.ReleaseComObject(releasing);}
            catch(Exception exception){RecordFailure(exception);}
        }
    }

    private static void RecordFailure(Exception exception)
    {
        // Branding is cosmetic: never turn a shell/diagnostics failure into a startup or close failure.
        try
        {
            Trace.TraceError($"Agent running taskbar identity: {exception}");
            string? directory=Environment.GetEnvironmentVariable("AGENT_STARTUP_DIAGNOSTICS");
            if(string.IsNullOrEmpty(directory))return;
            Directory.CreateDirectory(directory);
            File.AppendAllText(Path.Combine(directory,"taskbar-identity-error.txt"),$"{exception}\n");
        }
        catch{/* Diagnostics are best effort. */}
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct PropertyKey
    {
        public Guid FormatId;
        public uint PropertyId;
        public PropertyKey(uint propertyId){FormatId=new("9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3");PropertyId=propertyId;}
    }

    // Native x64 PROPVARIANT is 24 bytes (the counted-array union member is 16 bytes).
    // This app is win-x64 only; an 8-byte string pointer alone must not shrink that union.
    [StructLayout(LayoutKind.Explicit,Size=24)]
    private struct PropVariant
    {
        [FieldOffset(0)] public ushort VarType;
        [FieldOffset(8)] public IntPtr Pointer;
    }

    [ComImport,Guid("886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99"),InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    private interface IPropertyStore
    {
        [PreserveSig] int GetCount(out uint count);
        [PreserveSig] int GetAt(uint index,out PropertyKey key);
        [PreserveSig] int GetValue(ref PropertyKey key,out PropVariant value);
        [PreserveSig] int SetValue(ref PropertyKey key,ref PropVariant value);
        [PreserveSig] int Commit();
    }

    [DllImport("shell32.dll",ExactSpelling=true)]
    private static extern int SHGetPropertyStoreForWindow(IntPtr hwnd,ref Guid iid,[MarshalAs(UnmanagedType.Interface)] out IPropertyStore store);
}
