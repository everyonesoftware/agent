namespace Agent.Core;

public enum SettingsSection { Account, Provider, Research, Memory }

/// <summary>Account-bound drafts and mutation eligibility, shared with native view wiring.</summary>
public sealed class AccountViewState
{
    private readonly HashSet<SettingsSection> loaded = [];
    public void Reset(Action<string> clearPassword, Action clearPresentation)
    {
        loaded.Clear();
        foreach(string name in new[]{"Password","BraveKey","CurrentPassword","NewPassword","ConfirmPassword"})clearPassword(name);
        clearPresentation();
    }
    public void MarkLoaded(SettingsSection section) => loaded.Add(section);
    public bool IsLoaded(SettingsSection section) => loaded.Contains(section);
    public void Require(SettingsSection section)
    {
        if(!IsLoaded(section))throw new AgentException("Open Settings and refresh this section for the current account before making changes.");
    }
}
