namespace Agent.Core;

/// <summary>The OS/package boundary. Implementations must only offer this application's newer Windows packages.</summary>
public interface IWindowsUpdateBackend
{
    bool IsInstalled { get; }
    bool IsStartupCheckDeferred => false;
    Version CurrentVersion { get; }
    Task<Version?> CheckAsync();
    ReleaseNotes? OfferedNotes => null;
    Task<ReleaseNotes?> RetryNotesAsync(Version version) => Task.FromResult<ReleaseNotes?>(null);
    Task DownloadAsync(Version version, Action<int> progress, Action verifying, CancellationToken token);
    void ApplyAndRestart(Version version);
}

public enum WindowsUpdatePhase { Idle, Checking, Current, Available, Downloading, Verifying, Ready, Restarting, Error, NotInstalled }
public sealed record WindowsUpdateStatus(WindowsUpdatePhase Phase, string Message, Version? Version = null, int Progress = 0)
{
    public ReleaseNotes? Notes { get; init; }
    public bool RetryingNotes { get; init; }
}

/// <summary>Discovery is serialized with explicit download and restart. Exceptions never enter UI status text.</summary>
public sealed class WindowsUpdateController : IDisposable
{
    private readonly IWindowsUpdateBackend backend;
    private readonly Func<Task> preserveSession;
    private readonly CancellationTokenSource lifetime = new();
    private int busy;
    private int startupStarted;
    private volatile bool disposed;
    public WindowsUpdateStatus Status { get; private set; } = new(WindowsUpdatePhase.Idle, "Check for updates when you choose.");
    public event Action<WindowsUpdateStatus>? Changed;

    public WindowsUpdateController(IWindowsUpdateBackend backend, Func<Task> preserveSession)
    {
        this.backend = backend;
        this.preserveSession = preserveSession;
    }

    private void Set(WindowsUpdateStatus status)
    {
        if (disposed) return;
        Status = status;
        Changed?.Invoke(status);
    }

    public Task CheckAsync() => StartCheck(automatic: false);

    public async Task RetryChangelogAsync()
    {
        if (disposed || Interlocked.CompareExchange(ref busy, 1, 0) != 0) return;
        try
        {
            var offer = Status;
            if (offer.Phase != WindowsUpdatePhase.Available || offer.Version is not { } version || offer.Notes is not null) return;
            Set(offer with { RetryingNotes = true });
            ReleaseNotes? notes = null;
            try { notes = await Task.Run(() => backend.RetryNotesAsync(version)); }
            catch { /* Optional metadata does not invalidate the retained package. */ }
            Set(offer with { Notes = notes });
        }
        finally { Interlocked.Exchange(ref busy, 0); }
    }

    public Task CheckOnStartupAsync() => Interlocked.Exchange(ref startupStarted, 1) == 0
        ? StartCheck(automatic: true) : Task.CompletedTask;

    private Task StartCheck(bool automatic)
    {
        if (disposed || Interlocked.CompareExchange(ref busy, 1, 0) != 0) return Task.CompletedTask;
        return CheckReservedAsync(automatic);
    }

    private async Task CheckReservedAsync(bool automatic)
    {
        try
        {
            if (Status.Phase is WindowsUpdatePhase.Ready or WindowsUpdatePhase.Restarting) return;
            if (automatic && await Task.Run(() => backend.IsStartupCheckDeferred)) return;
            Set(new(WindowsUpdatePhase.Checking, "Looking for a newer Windows version…"));
            var result = await Task.Run(async () =>
            {
                if (!backend.IsInstalled)
                    return new WindowsUpdateStatus(WindowsUpdatePhase.NotInstalled, "Install Agent using the Windows installer to enable in-app updates. Loose development builds cannot update.");
                var version = await backend.CheckAsync();
                return version is not null && version > backend.CurrentVersion
                    ? new WindowsUpdateStatus(WindowsUpdatePhase.Available, $"Agent {version}", version) { Notes = backend.OfferedNotes }
                    : new(WindowsUpdatePhase.Current, "No newer Windows version is available.");
            });
            Set(result);
        }
        catch { Set(new(WindowsUpdatePhase.Error, "Could not check for updates. Check your internet connection or try again later (GitHub may be rate-limiting requests).")); }
        finally { Interlocked.Exchange(ref busy, 0); }
    }


    public async Task DownloadAsync()
    {
        if (disposed || Interlocked.CompareExchange(ref busy, 1, 0) != 0) return;
        try
        {
            if (Status.Phase != WindowsUpdatePhase.Available || Status.Version is not { } version) return;
            var offer = Status;
            Set(offer with { Phase = WindowsUpdatePhase.Downloading, Message = "", Progress = 0 });
            await Task.Run(() => backend.DownloadAsync(version, value =>
            {
                if (Status.Phase == WindowsUpdatePhase.Downloading)
                    Set(offer with { Phase = WindowsUpdatePhase.Downloading, Message = "", Progress = Math.Clamp(value, 0, 100) });
            }, () => Set(offer with { Phase = WindowsUpdatePhase.Verifying, Message = "", Progress = 100 }), lifetime.Token));
            Set(offer with { Phase = WindowsUpdatePhase.Ready, Message = "", Progress = 100 });
        }
        catch { Set(new(WindowsUpdatePhase.Error, "Download failed or was cancelled. Nothing was installed. Check for updates to try again.")); }
        finally { Interlocked.Exchange(ref busy, 0); }
    }

    public async Task RestartAsync()
    {
        if (disposed || Interlocked.CompareExchange(ref busy, 1, 0) != 0) return;
        var ready = Status;
        try
        {
            if (ready.Phase != WindowsUpdatePhase.Ready || ready.Version is not { } version) return;
            Set(ready with { Phase = WindowsUpdatePhase.Restarting, Message = "Saving your session before restarting…" });
            await preserveSession();
            if (!disposed) backend.ApplyAndRestart(version);
        }
        catch { Set(ready with { Message = "Could not save your session or start the installer. Agent has not requested another restart. Try Restart and install again." }); }
        finally { Interlocked.Exchange(ref busy, 0); }
    }

    public void Dispose()
    {
        disposed = true;
        lifetime.Cancel();
        // Keep the CTS alive while a download may still be observing its token.
    }
}
