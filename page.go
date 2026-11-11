package main

const homePage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="dark">
  <title>Agent</title>
  <style>
    :root { color-scheme: dark; font: 14px/1.6 'Segoe UI', Arial, sans-serif; color: #e1e6e2; background: #202322; --muted: #a0aaa3; --line: #333936; --accent: #b4d6a3; --field: #252a27; }
    * { box-sizing: border-box; }
    html, body { margin: 0; width: 100%; height: 100%; overflow: hidden; }
    .visually-hidden { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
    /* Focus: a bounded workspace with independent history and message scroll. */
    [hidden] { display: none !important; }
    button, input, textarea, select { font: inherit; }
    button { color: inherit; border: 1px solid transparent; border-radius: 5px; background: transparent; cursor: pointer; }
    header { display: flex; align-items: center; justify-content: space-between; }
    button:focus-visible, input:focus-visible, textarea:focus-visible, select:focus-visible, a:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
    button:disabled { opacity: .5; cursor: wait; }
    a { color: var(--accent); }
    .brand { display: flex; align-items: center; gap: 9px; color: #e1e6e2; font-size: 15px; font-weight: 600; letter-spacing: -.4px; }
    .mark { display: grid; place-items: center; width: 26px; height: 26px; flex-shrink: 0; border-radius: 6px; color: #1c281a; background: var(--accent); font: 17px Georgia, serif; letter-spacing: -1.2px; padding-right: 1px; }
    .app { position: relative; display: grid; grid-template-columns: 232px minmax(0, 1fr); width: 100%; height: 100vh; height: 100dvh; margin: 0; border: 0; background: #202322; }
    .sidebar { display: flex; flex-direction: column; gap: 18px; min-height: 0; padding: 25px 14px 12px; border-right: 1px solid var(--line); background: #181b1a; }
    .sidebar .brand { padding: 0 6px 8px; }
    .reset, .connect { padding: 9px 12px; color: #e1e6e2; background: var(--field); border: 1px solid var(--line); font-size: 12px; font-weight: 400; }
    .reset:hover:not(:disabled), .connect:hover:not(:disabled) { background: #2c3930; }
    .new-chat { text-align: left; width: 100%; }
    .search-label { display: block; }
    .search-label input { width: 100%; min-width: 0; padding: 9px 10px; border: 1px solid var(--line); border-radius: 5px; background: transparent; color: inherit; font-size: 12px; }
    .history { position: relative; overflow-y: auto; flex: 1; min-height: 0; }
    .history-group { color: var(--muted); font-size: 10px; font-weight: 500; margin: 18px 9px 6px; }
    .history-group:first-child { margin-top: 0; }
    .session { width: 100%; display: block; padding: 10px; text-align: left; font-size: 12px; overflow-wrap: anywhere; }
    .session:hover { background: var(--field); }
    .session[aria-current="true"] { color: var(--accent); background: #2c3930; }
    .session small { display: block; color: var(--muted); font-size: 10px; margin-top: 3px; }
    .history-state { color: var(--muted); font-size: 12px; padding: 6px 9px; }
    .history-state.error { color: #efada5; }
    .account-area { position: relative; border-top: 1px solid var(--line); padding-top: 10px; }
    .account-row { display: flex; align-items: center; gap: 4px; }
    .account-toggle { flex: 1; min-width: 0; display: flex; align-items: center; gap: 10px; padding: 8px 6px; text-align: left; font-size: 12px; }
    .account-toggle > span:last-child { min-width: 0; }
    .settings-button { display: grid; place-items: center; flex: 0 0 36px; height: 36px; color: var(--muted); }
    .settings-button:hover, .settings-button[aria-pressed="true"] { color: var(--accent); background: var(--field); }
    .avatar { display: grid; place-items: center; width: 30px; height: 30px; border-radius: 50%; background: #2c322e; color: var(--muted); }
    .account-toggle small { display: block; color: var(--muted); font-size: 10px; }
    #current-username { display: block; max-width: 140px; overflow: hidden; text-overflow: ellipsis; }
    .account-menu { position: absolute; z-index: 12; bottom: calc(100% + 8px); left: 0; width: 300px; max-width: calc(100vw - 28px); max-height: calc(100dvh - 100px); overflow-y: auto; padding: 16px; background: #252a27; border: 1px solid var(--line); border-radius: 7px; box-shadow: 0 12px 35px #0005; }
    .account-menu h2 { margin: 0 0 10px; font-size: 13px; font-weight: 600; }
    .account-menu .model-select { width: 100%; max-width: 100%; margin-bottom: 8px; }
    .provider-actions { display: flex; gap: 7px; flex-wrap: wrap; }
    .connection-status { margin: 0 0 12px; padding: 0; border: 0; color: var(--muted); font-size: 11px; overflow-wrap: anywhere; }
    .menu-error { margin: 8px 0; color: #efada5; font-size: 12px; }
    .settings-panel { position: relative; flex: 1; min-height: 0; overflow-y: auto; padding: 30px; }
    .settings-content { max-width: 600px; margin: 0 auto; }
    .settings-content > p, .settings-card > p { color: var(--muted); font-size: 12px; }
    .settings-card { margin-top: 24px; padding: 22px; border: 1px solid var(--line); border-radius: 8px; background: var(--field); }
    .settings-card h2 { margin: 0 0 6px; font-size: 16px; font-weight: 500; }
    .settings-card label { display: block; margin: 16px 0 6px; font-size: 12px; }
    .settings-card input { display: block; width: 100%; padding: 10px 12px; color: inherit; background: #202322; border: 1px solid var(--line); border-radius: 5px; }
    .settings-card .send { margin: 16px 0 0; }
    .connection-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 6px; }
    .connection-heading h2 { margin: 0; }
    .connection-badge { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; font-size: 10px; color: var(--accent); border: 1px solid #45513f; border-radius: 20px; padding: 3px 8px; }
    .connection-badge::before { content: ''; width: 5px; height: 5px; border-radius: 50%; background: currentColor; }
    .connection-badge.warning { color: #dfc38e; border-color: #5c513b; }
    .connection-badge.off { color: var(--muted); border-color: var(--line); }
    .settings-card #account-select { display: block; width: 100%; max-width: 100%; min-height: 40px; margin-bottom: 14px; font-size: 12px; background: #202322; }
    .settings-card .connection-status { margin: 14px 0 3px; color: inherit; font-size: 12px; }
    .connection-detail { margin: 0; color: var(--muted); font-size: 11px; }
    .settings-card .provider-actions { margin-top: 18px; gap: 8px; align-items: center; }
    .settings-card .provider-actions .send { margin: 0; }
    .danger { color: #efada5; }
    .provider-actions .danger { margin-left: auto; background: transparent; }
    .connection-note { border-top: 1px solid var(--line); margin-top: 18px; padding-top: 13px; color: var(--muted); font-size: 11px; }
    #provider-feedback { margin: 14px 0 0; color: var(--accent); font-size: 12px; }
    #provider-feedback.warning { color: #dfc38e; }
    .security-toggle { display: flex; align-items: center; justify-content: space-between; width: 100%; padding: 0; text-align: left; }
    .security-toggle h2 { margin: 0; }
    .security-copy { margin: 7px 0 0; }
    .connection-dialog { width: 420px; max-width: calc(100% - 32px); max-height: calc(100dvh - 32px); overflow-y: auto; padding: 24px; border: 1px solid #465049; border-radius: 8px; background: var(--field); color: #e1e6e2; }
    .connection-dialog::backdrop { background: #0009; }
    .connection-dialog h2 { margin: 0 0 12px; font-size: 16px; font-weight: 500; }
    .connection-dialog p { font-size: 12px; color: var(--muted); overflow-wrap: anywhere; }
    .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
    #password-status { margin: 12px 0 0; font-size: 12px; color: var(--accent); }
    #password-status.error { color: #efada5; }
    #settings-title { margin: 0; font-size: 13px; font-weight: 500; }
    .workspace { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
    .workspace-header { min-height: 65px; padding: 12px 30px; border-bottom: 1px solid var(--line); display: flex; justify-content: flex-start; gap: 12px; }
    #conversation-title { margin: 0; font-size: 13px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .sidebar-toggle, .sidebar-backdrop { display: none; }
    .messages { position: relative; flex: 1; min-height: 0; overflow-y: auto; padding: 30px max(24px, calc((100% - 800px) / 2)); scroll-behavior: auto; }
    .welcome { max-width: 540px; margin: clamp(25px, 12vh, 130px) auto 0; text-align: left; }
    .welcome h1 { margin: 0 0 8px; font-size: clamp(24px, 3vw, 32px); font-weight: 450; letter-spacing: -.8px; }
    .welcome p { color: var(--muted); font-size: 13px; }
    .message, .message.user, .message.assistant { width: 100%; max-width: 800px; padding: 0; margin: 0 auto 30px; border: 0; border-radius: 0; background: transparent; }
    .sender { margin-bottom: 8px; font-size: 11px; color: var(--muted); font-weight: 500; text-transform: none; letter-spacing: 0; }
    .message.user .message-text { color: #c1cbc3; }
    .message-text { font-size: 14px; line-height: 1.85; white-space: pre-wrap; overflow-wrap: anywhere; }
    .composer-wrap { width: 100%; max-width: 856px; margin: 0 auto; padding: 10px 28px 18px; border: 0; }
    .notice { min-height: 18px; color: var(--muted); font-size: 11px; margin: 0 0 8px; }
    .notice.error { color: #efada5; }
    .composer { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; padding: 12px; border: 1px solid var(--line); border-radius: 8px; background: var(--field); box-shadow: none; }
    textarea { width: 100%; flex: 1 0 100%; min-width: 0; min-height: 52px; max-height: min(150px, 22dvh); padding: 3px; border: 0; resize: vertical; outline: none; color: inherit; background: transparent; }
    textarea::placeholder { color: var(--muted); }
    .model-select { min-width: 0; max-width: min(70%, 300px); padding: 6px 8px; border: 1px solid var(--line); border-radius: 5px; background: var(--field); color: #c5cec7; font-size: 11px; }
    .send { margin-left: auto; min-width: 54px; padding: 7px 12px; border-radius: 5px; background: var(--accent); color: #1c281a; font-size: 12px; font-weight: 600; }
    .send:hover:not(:disabled) { background: #c4e5b5; }
    .hint { margin: 7px 0 0; color: var(--muted); font-size: 10px; text-align: center; }
    .auth-gate { width: 100%; height: 100vh; height: 100dvh; margin: 0; padding: 0; border: 0; border-radius: 0; background: #202322; display: flex; flex-direction: column; overflow-y: auto; }
    .auth-header { min-height: 90px; padding: 20px 44px; border: 0; }

    .auth-center { flex: 1; display: grid; place-items: center; padding: 30px 24px; }
    .auth-content { width: min(360px, 100%); }
    .auth-content h1 { margin: 0 0 8px; font-size: 32px; line-height: 1.3; font-weight: 450; letter-spacing: -1px; }
    #auth-intro { margin: 0 0 32px; color: var(--muted); font-size: 13px; }
    .auth-content label { display: block; margin: 18px 0 8px; font-size: 12px; }
    .auth-gate input { margin: 0; width: 100%; padding: 11px 12px; color: inherit; background: var(--field); border: 1px solid var(--line); border-radius: 5px; font-size: 13px; }
    .password-field { position: relative; }
    .password-field input { padding-right: 65px; }
    .auth-gate #toggle-password { position: absolute; right: 4px; top: 3px; bottom: 3px; padding: 5px 9px; background: transparent; color: var(--muted); font-size: 11px; font-weight: 400; }
    #password-hint { color: var(--muted); font-size: 11px; margin: 7px 0; }
    .auth-error { min-height: 20px; margin: 14px 0 8px; color: #efada5; font-size: 12px; }
    .auth-gate #account-submit { width: 100%; min-height: 44px; background: var(--accent); color: #1c281a; font-size: 13px; font-weight: 600; border-radius: 5px; }
    .auth-switch { margin: 24px 0 0; text-align: center; color: var(--muted); font-size: 12px; }
    .auth-gate #toggle-account-mode { padding: 4px; background: none; color: var(--accent); font-weight: 400; font-size: 12px; }

    @media (max-width: 700px) {
      .app { display: block; }
      .workspace { height: 100%; }
      .sidebar { display: none; position: absolute; inset: 0 auto 0 0; width: 268px; max-width: calc(100% - 56px); z-index: 20; }
      .app.sidebar-open .sidebar { display: flex; }
      .sidebar-toggle { display: block; min-width: 44px; min-height: 44px; font-size: 20px; }
      .app.sidebar-open .sidebar-backdrop { display: block; position: absolute; inset: 0; z-index: 19; width: 100%; border: 0; border-radius: 0; background: #0008; }
      .workspace-header { min-height: 60px; padding: 8px 12px; }
      .messages { padding: 24px 21px; }
      .settings-panel { padding: 22px 18px; }
      .settings-card { padding: 18px; }
      .settings-button { flex-basis: 44px; height: 44px; }
      .settings-panel button, .settings-panel select, .connection-dialog button { min-height: 44px; }
      .composer-wrap { padding: 8px 14px 12px; }
      .session, .account-toggle, .new-chat { min-height: 44px; }
      .account-menu button, .account-menu select { min-height: 44px; }
      .auth-header { min-height: 75px; padding: 18px 25px; }
      .auth-center { padding: 20px 25px; }
    }
    @media (prefers-reduced-motion: reduce) { * { scroll-behavior: auto !important; } }
  </style>
</head>
<body>
  <main class="auth-gate" id="auth-gate">
    <header class="auth-header"><div class="brand"><span class="mark" aria-hidden="true">e1</span>Agent</div></header>
    <div class="auth-center"><div class="auth-content">
    <h1 id="auth-title">Welcome back.</h1>
    <p id="auth-intro">Sign in to pick up the conversation.</p>
    <form id="account-form">
      <label for="username-input">Username</label>
      <input id="username-input" name="username" minlength="3" maxlength="64" autocomplete="username" required>
      <label for="password-input">Password</label>
      <div class="password-field"><input id="password-input" name="password" type="password" minlength="12" maxlength="72" autocomplete="current-password" aria-describedby="password-hint" required><button id="toggle-password" type="button" aria-label="Show password" aria-pressed="false">Show</button></div>
      <p id="password-hint" hidden>Use at least 12 characters.</p>
      <p class="auth-error" id="auth-error" role="alert"></p>
      <button id="account-submit" type="submit">Sign in</button>
    </form>
    <p class="auth-switch"><span id="account-mode-label">New to Agent?</span> <button type="button" id="toggle-account-mode">Create an account</button></p>
    </div></div>

  </main>
  <main class="app" id="chat-app" hidden>
    <button id="sidebar-backdrop" class="sidebar-backdrop" type="button" aria-label="Close session navigation" tabindex="-1" hidden></button>
    <aside class="sidebar" id="session-sidebar" aria-label="Session navigation">
      <div class="brand"><span class="mark" aria-hidden="true">e1</span>Agent</div>
      <button class="reset new-chat" id="new-chat" type="button">＋ New conversation</button>
      <label class="search-label"><span class="visually-hidden">Search conversations</span><input type="search" id="conversation-search" placeholder="Search conversations" autocomplete="off"></label>
      <nav class="history" id="conversation-list" aria-label="Previous sessions"><p class="history-state">Loading conversations…</p></nav>
      <div class="account-area">
      <div class="account-row"><button class="account-toggle" id="account-menu-toggle" type="button" aria-expanded="false" aria-controls="account-menu"><span class="avatar" id="user-avatar" aria-hidden="true">?</span><span><strong id="current-username"></strong><small>Your account</small></span></button>
      <button class="settings-button" id="settings-button" type="button" aria-label="Settings" title="Settings" aria-controls="settings-panel" aria-pressed="false"><svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 3-.6 2.2-1.5.9-2.2-.6-3 5.2 1.6 1.6v1.8l-1.6 1.6 3 5.2 2.2-.6 1.5.9L9 23h6l.6-2.2 1.5-.9 2.2.6 3-5.2-1.6-1.6v-1.8l1.6-1.6-3-5.2-2.2.6-1.5-.9L15 3Z" transform="translate(1 0) scale(.92)"/><circle cx="12" cy="12" r="3.2"/></svg></button></div>
      <section class="account-menu" id="account-menu" aria-label="Account settings" hidden>
        <h2>Your account</h2>
        <button class="reset" id="manage-connections" type="button">Manage connections &amp; settings</button>
      </section></div>
    </aside>
    <div class="workspace">
    <header class="workspace-header"><button id="sidebar-toggle" class="sidebar-toggle" type="button" aria-label="Open session navigation" aria-controls="session-sidebar" aria-expanded="false">☰</button><h1 id="conversation-title">New conversation</h1><h1 id="settings-title" hidden>Settings</h1></header>
    <section class="settings-panel" id="settings-panel" aria-labelledby="settings-title" hidden>
      <div class="settings-content">
        <button class="reset" id="back-to-chat" type="button">← Back to conversation</button>
        <p>Manage your connections, account, and security.</p>
        <section class="settings-card" aria-labelledby="connection-title">
          <div class="connection-heading"><h2 id="connection-title">ChatGPT subscription</h2><span class="connection-badge off" id="connection-badge">Checking…</span></div>
          <p>Manage the ChatGPT account Agent uses for conversations.</p>
          <label id="account-select-label" for="account-select" hidden>Active ChatGPT account</label>
          <select class="model-select" id="account-select" hidden disabled><option value="">Loading accounts…</option></select>
          <p class="connection-status" id="connection-status" role="status">Checking ChatGPT connection…</p>
          <p class="connection-detail">This connection is separate from your Agent sign-in.</p>
          <div class="provider-actions">
            <button class="send" id="connect-chatgpt" type="button" hidden>Continue with ChatGPT ↗</button>
            <button class="reset" id="add-chatgpt-account" type="button" hidden>Add account ↗</button>
            <button class="reset" id="check-chatgpt" type="button">Check connection</button>
            <button class="reset danger" id="disconnect-chatgpt" type="button" hidden>Disconnect</button>
          </div>
          <p id="provider-error" class="menu-error" role="alert" hidden></p>
          <p id="provider-feedback" role="status" aria-live="polite" hidden></p>
          <div class="connection-note" id="connection-note">Adding or reconnecting an account opens ChatGPT authorization in your browser. No password is entered in Agent.</div>
        </section>
        <section class="settings-card" aria-labelledby="password-title">
          <button class="security-toggle" id="security-toggle" type="button" aria-expanded="false" aria-controls="password-form"><h2 id="password-title">Reset password</h2><span id="security-chevron" aria-hidden="true">＋</span></button>
          <p class="security-copy">Change your Agent password, not your ChatGPT password.</p>
          <form id="password-form" hidden>
            <p class="connection-detail">Your other Agent sessions will be signed out.</p>
            <label for="current-password">Current password</label>
            <input id="current-password" name="current_password" type="password" autocomplete="current-password" maxlength="72" required>
            <label for="new-password">New password</label>
            <input id="new-password" name="new_password" type="password" autocomplete="new-password" aria-describedby="new-password-hint" required>
            <p id="new-password-hint" class="hint" style="text-align:left">Use at least 12 characters (up to 72 bytes).</p>
            <label for="confirm-password">Confirm new password</label>
            <input id="confirm-password" name="confirm_password" type="password" autocomplete="new-password" required>
            <button class="send" id="password-submit" type="submit">Update password</button>
            <p id="password-status" role="status" aria-live="polite"></p>
          </form>
        </section>
        <section class="settings-card" aria-labelledby="signout-title">
          <h2 id="signout-title">Sign out</h2>
          <p>Sign out of Agent on this browser. Your saved conversations will still be here when you return.</p>
          <button class="reset" id="logout-button" type="button">Sign out</button>
          <p id="logout-error" class="menu-error" role="alert" hidden></p>
        </section>
      </div>
    </section>
    <section class="messages" id="messages" aria-live="polite" aria-relevant="additions text" aria-label="Conversation">
      <div class="welcome" id="welcome">
        <h1>What can I help with?</h1>
        <p>Ask a question to start a conversation.</p>
      </div>
    </section>
    <div class="composer-wrap">
      <p class="notice" id="notice" role="status">Messages and replies are saved to your account after each successful request.</p>
      <form class="composer" id="chat-form" autocomplete="off">
        <label class="visually-hidden" for="message-input">Message</label>
        <textarea id="message-input" name="message" rows="1" maxlength="8192" placeholder="Message Agent" required></textarea>
        <label class="visually-hidden" for="model-select">Model</label>
        <select class="model-select" id="model-select" disabled><option value="">Loading models…</option></select>
        <button class="send" id="send-button" type="submit">Send</button>
      </form>
      <p class="hint">Enter to send · Shift+Enter for a new line</p>
    </div>
    </div>
  </main>
  <dialog class="connection-dialog" id="disconnect-dialog" aria-labelledby="disconnect-title" aria-describedby="disconnect-description">
    <h2 id="disconnect-title">Disconnect ChatGPT?</h2>
    <p id="disconnect-account"></p>
    <p id="disconnect-description">Your saved conversations and ChatGPT subscription are not deleted. This does not sign you out of Agent.</p>
    <div class="dialog-actions"><button class="reset" id="cancel-disconnect" type="button" autofocus>Cancel</button><button class="reset danger" id="confirm-disconnect" type="button">Disconnect account</button></div>
  </dialog>
  <script>
    (function () {
      const form = document.getElementById('chat-form');
      const input = document.getElementById('message-input');
      const sendButton = document.getElementById('send-button');
      const messageList = document.getElementById('messages');
      const notice = document.getElementById('notice');
      const connectionStatus = document.getElementById('connection-status');
      const modelSelect = document.getElementById('model-select');
      const accountSelect = document.getElementById('account-select');
      const connectButton = document.getElementById('connect-chatgpt');
      const addAccountButton = document.getElementById('add-chatgpt-account');
      const disconnectButton = document.getElementById('disconnect-chatgpt');
      const checkConnectionButton = document.getElementById('check-chatgpt');
      const connectionBadge = document.getElementById('connection-badge');
      const providerError = document.getElementById('provider-error');
      const providerFeedback = document.getElementById('provider-feedback');
      const disconnectDialog = document.getElementById('disconnect-dialog');
      let connectionBusy = false;
      let connectionRefresh = null;
      let activeProfileID = '';
      let selectedProfile = null;
      let disconnectProfileID = '';
      let profileCount = 0;
      let connectionAvailable = false;
      let pendingProfiles = [];
      const localHost = ['localhost', '127.0.0.1', '::1', '[::1]'].includes(window.location.hostname);
      const conversation = [];
      let busy = false;
      let loadingConversation = false;
      let conversationVersion = 0;
      let savedConversations = [];
      let historyError = '';
      let historyRequest = 0;
      let conversationId = '';
      let modelStorageKey = 'everyoneagent.model';

      const authGate = document.getElementById('auth-gate');
      const chatApp = document.getElementById('chat-app');
      const accountForm = document.getElementById('account-form');
      const authError = document.getElementById('auth-error');
      const accountSubmit = document.getElementById('account-submit');
      const accountModeButton = document.getElementById('toggle-account-mode');
      let registering = false;
      let authBusy = false;
      const usernameInput = document.getElementById('username-input');
      const passwordInput = document.getElementById('password-input');
      const passwordToggle = document.getElementById('toggle-password');
      const conversationTitle = document.getElementById('conversation-title');
      const conversationList = document.getElementById('conversation-list');
      const conversationSearch = document.getElementById('conversation-search');
      const sidebar = document.getElementById('session-sidebar');
      const sidebarToggle = document.getElementById('sidebar-toggle');
      const backdrop = document.getElementById('sidebar-backdrop');
      const accountMenu = document.getElementById('account-menu');
      const accountToggle = document.getElementById('account-menu-toggle');
      const mobile = window.matchMedia('(max-width: 700px)');
      const settingsPanel = document.getElementById('settings-panel');
      const settingsButton = document.getElementById('settings-button');
      const passwordForm = document.getElementById('password-form');
      const passwordStatus = document.getElementById('password-status');
      const currentPassword = document.getElementById('current-password');
      const newPassword = document.getElementById('new-password');
      const confirmPassword = document.getElementById('confirm-password');
      let passwordBusy = false;

      function showSettings(open) {
        settingsPanel.hidden = !open;
        document.getElementById('settings-title').hidden = !open;
        conversationTitle.hidden = open;
        messageList.hidden = open;
        document.querySelector('.composer-wrap').hidden = open;
        settingsButton.setAttribute('aria-pressed', String(open));
        closeAccount(false);
        setSidebar(false, false);
        if (!passwordBusy) { passwordForm.reset(); passwordStatus.textContent = ''; }
        if (open) { document.getElementById('back-to-chat').focus(); if (!connectionBusy) refreshConnectionState(); }
        else input.focus();
      }
      settingsButton.addEventListener('click', function () { showSettings(true); });
      document.getElementById('manage-connections').addEventListener('click', function () { showSettings(true); });
      document.getElementById('back-to-chat').addEventListener('click', function () { showSettings(false); });
      document.getElementById('security-toggle').addEventListener('click', function () {
        if (passwordBusy) return;
        const open = passwordForm.hidden;
        passwordForm.hidden = !open;
        this.setAttribute('aria-expanded', String(open));
        document.getElementById('security-chevron').textContent = open ? '−' : '＋';
        if (!open) { passwordForm.reset(); passwordStatus.textContent = ''; }
      });
      passwordForm.addEventListener('submit', async function (event) {
        event.preventDefault();
        if (passwordBusy) return;
        passwordStatus.classList.add('error');
        if (newPassword.value !== confirmPassword.value) { passwordStatus.textContent = 'New passwords do not match.'; confirmPassword.focus(); return; }
        if (Array.from(newPassword.value).length < 12 || new TextEncoder().encode(newPassword.value).length > 72) { passwordStatus.textContent = 'Use at least 12 characters and no more than 72 bytes.'; newPassword.focus(); return; }
        passwordBusy = true;
        passwordStatus.textContent = '';
        const button = document.getElementById('password-submit');
        const controls = Array.from(passwordForm.elements);
        controls.forEach(control => { control.disabled = true; });
        passwordForm.setAttribute('aria-busy', 'true');
        button.textContent = 'Updating…';
        try {
          const response = await fetch('/api/auth/password', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ current_password: currentPassword.value, new_password: newPassword.value })
          });
          const result = await response.json();
          if (!response.ok) throw new Error(result.error || 'Password could not be updated. Please try again.');
          passwordForm.reset();
          passwordStatus.classList.remove('error');
          passwordStatus.textContent = 'Password updated. Your other sessions have been signed out.';
        } catch (error) { passwordStatus.textContent = error.message || 'Password could not be updated. Please try again.'; }
        finally {
          passwordBusy = false;
          controls.forEach(control => { control.disabled = false; });
          passwordForm.setAttribute('aria-busy', 'false');
          button.textContent = 'Update password';
          if (settingsPanel.hidden) { passwordForm.reset(); passwordStatus.textContent = ''; }
        }
      });

      function closeAccount(restoreFocus) {
        accountMenu.hidden = true;
        accountToggle.setAttribute('aria-expanded', 'false');
        if (restoreFocus) accountToggle.focus();
      }
      function setSidebar(open, restoreFocus) {
        const expanded = mobile.matches && open;
        chatApp.classList.toggle('sidebar-open', expanded);
        sidebarToggle.setAttribute('aria-expanded', String(expanded));
        sidebarToggle.setAttribute('aria-label', expanded ? 'Close session navigation' : 'Open session navigation');
        backdrop.hidden = !expanded;
        sidebar.inert = mobile.matches && !expanded;
        if (!expanded) closeAccount(false);
        if (expanded) conversationSearch.focus();
        else if (restoreFocus && mobile.matches) sidebarToggle.focus();
      }
      sidebarToggle.addEventListener('click', function () { setSidebar(!chatApp.classList.contains('sidebar-open'), true); });
      backdrop.addEventListener('click', function () { setSidebar(false, true); });
      mobile.addEventListener('change', function () { setSidebar(false, false); });
      setSidebar(false, false);
      accountToggle.addEventListener('click', function () {
        const open = accountMenu.hidden;
        accountMenu.hidden = !open;
        accountToggle.setAttribute('aria-expanded', String(open));
      });
      document.addEventListener('click', function (event) {
        if (!accountMenu.hidden && !event.target.closest('.account-area')) closeAccount(false);
      });
      document.addEventListener('keydown', function (event) {
        if (event.key === 'Escape') {
          if (!accountMenu.hidden) { closeAccount(true); event.preventDefault(); }
          else if (chatApp.classList.contains('sidebar-open')) { setSidebar(false, true); event.preventDefault(); }
        }
        if (event.key === 'Tab' && mobile.matches && chatApp.classList.contains('sidebar-open')) {
          const controls = Array.from(sidebar.querySelectorAll('button, input, select, a[href]')).filter(el => !el.disabled && el.getClientRects().length);
          const first = controls[0], last = controls[controls.length - 1];
          if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
          else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
        }
      });
      passwordToggle.addEventListener('click', function () {
        const visible = passwordInput.type === 'password';
        passwordInput.type = visible ? 'text' : 'password';
        passwordToggle.setAttribute('aria-pressed', String(visible));
        passwordToggle.setAttribute('aria-label', visible ? 'Hide password' : 'Show password');
        passwordToggle.textContent = visible ? 'Hide' : 'Show';
      });

      accountModeButton.addEventListener('click', function () {
        if (authBusy) return;
        registering = !registering;
        document.getElementById('auth-title').textContent = registering ? 'Make yourself at home.' : 'Welcome back.';
        document.getElementById('auth-intro').textContent = registering ? 'Create your personal Agent account.' : 'Sign in to pick up the conversation.';
        document.getElementById('account-mode-label').textContent = registering ? 'Already have an account?' : 'New to Agent?';
        document.getElementById('password-hint').hidden = !registering;
        accountSubmit.textContent = registering ? 'Create account' : 'Sign in';
        accountModeButton.textContent = registering ? 'Sign in' : 'Create an account';
        accountForm.reset();
        passwordInput.autocomplete = registering ? 'new-password' : 'current-password';
        passwordInput.type = 'password';
        passwordToggle.setAttribute('aria-pressed', 'false');
        passwordToggle.setAttribute('aria-label', 'Show password');
        passwordToggle.textContent = 'Show';
        authError.textContent = '';
        usernameInput.removeAttribute('aria-invalid');
        passwordInput.removeAttribute('aria-invalid');
        usernameInput.focus();
      });

      accountForm.addEventListener('submit', async function (event) {
        event.preventDefault();
        if (authBusy) return;
        const credentials = { username: usernameInput.value, password: passwordInput.value };
        authBusy = true;
        for (const control of [accountSubmit, accountModeButton, usernameInput, passwordInput, passwordToggle]) control.disabled = true;
        accountForm.setAttribute('aria-busy', 'true');
        accountSubmit.setAttribute('aria-busy', 'true');
        accountSubmit.textContent = registering ? 'Creating account…' : 'Signing in…';
        authError.textContent = '';
        try {
          const response = await fetch(registering ? '/api/auth/register' : '/api/auth/login', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(credentials)
          });
          const result = await response.json();
          if (!response.ok) throw new Error(result.error || 'Account request failed.');
          window.location.reload();
        } catch (error) {
          authError.textContent = error.message || 'Account request failed.';
          usernameInput.setAttribute('aria-invalid', 'true');
          passwordInput.setAttribute('aria-invalid', 'true');
        } finally {
          authBusy = false;
          for (const control of [accountSubmit, accountModeButton, usernameInput, passwordInput, passwordToggle]) control.disabled = false;
          accountForm.setAttribute('aria-busy', 'false');
          accountSubmit.setAttribute('aria-busy', 'false');
          accountSubmit.textContent = registering ? 'Create account' : 'Sign in';
        }
      });

      function setInteractionState() {
        const locked = busy || loadingConversation;
        sendButton.disabled = locked;
        document.getElementById('new-chat').disabled = locked;
        for (const button of conversationList.querySelectorAll('.session')) button.disabled = locked;
        form.setAttribute('aria-busy', String(locked));
      }

      function historyGroup(seconds) {
        const date = new Date(Number(seconds) * 1000);
        if (!Number.isFinite(date.getTime()) || !seconds) return 'Earlier';
        const today = new Date(); today.setHours(0, 0, 0, 0);
        const yesterday = new Date(today); yesterday.setDate(yesterday.getDate() - 1);
        const week = new Date(today); week.setDate(week.getDate() - 7);
        if (date >= today) return 'Today';
        if (date >= yesterday) return 'Yesterday';
        if (date >= week) return 'Previous 7 days';
        return 'Earlier';
      }

      function renderConversations() {
        conversationList.replaceChildren();
        if (historyError) {
          const error = document.createElement('p'); error.className = 'history-state error'; error.textContent = historyError;
          const retry = document.createElement('button'); retry.type = 'button'; retry.className = 'reset'; retry.textContent = 'Retry';
          retry.addEventListener('click', function () { void refreshConversations(); });
          conversationList.append(error, retry);
        }
        const query = conversationSearch.value.trim().toLocaleLowerCase();
        const matches = savedConversations.filter(item => (item.title || 'Conversation').toLocaleLowerCase().includes(query));
        for (const group of ['Today', 'Yesterday', 'Previous 7 days', 'Earlier']) {
          const items = matches.filter(item => historyGroup(item.updated_at) === group);
          if (!items.length) continue;
          const heading = document.createElement('h2'); heading.className = 'history-group'; heading.textContent = group; conversationList.appendChild(heading);
          for (const item of items) {
            const button = document.createElement('button'); button.type = 'button'; button.className = 'session';
            button.textContent = item.title || 'Conversation';
            button.dataset.conversationId = item.id;
            if (item.id === conversationId) button.setAttribute('aria-current', 'true');
            const date = new Date(Number(item.updated_at) * 1000);
            if (item.updated_at && Number.isFinite(date.getTime())) { const label = document.createElement('small'); label.textContent = date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }); button.appendChild(label); }
            button.addEventListener('click', function () { void loadConversation(item); });
            conversationList.appendChild(button);
          }
        }
        if (!matches.length && !historyError) {
          const empty = document.createElement('p'); empty.className = 'history-state'; empty.textContent = query ? 'No matching conversations.' : 'No saved conversations yet.'; conversationList.appendChild(empty);
        }
        setInteractionState();
      }
      conversationSearch.addEventListener('input', renderConversations);

      async function refreshConversations() {
        const request = ++historyRequest;
        try {
          const response = await fetch('/api/conversations', { cache: 'no-store' });
          const result = await response.json();
          if (!response.ok || !Array.isArray(result.conversations)) throw new Error('load');
          if (request !== historyRequest) return;
          savedConversations = result.conversations.filter(item => item && item.id).sort((a, b) => Number(b.updated_at || 0) - Number(a.updated_at || 0));
          historyError = '';
          const current = savedConversations.find(item => item.id === conversationId);
          if (current) conversationTitle.textContent = current.title || 'Conversation';
        } catch (_) { if (request !== historyRequest) return; historyError = 'Saved conversations could not be loaded.'; }
        renderConversations();
      }

      async function requireSignIn() {
        try {
          const response = await fetch('/api/auth/me', { cache: 'no-store' });
          if (!response.ok) throw new Error('signed out');
          const user = await response.json();
          modelStorageKey = 'everyoneagent.model.' + user.id;
          document.getElementById('current-username').textContent = user.username;
          document.getElementById('user-avatar').textContent = Array.from(user.username || '?')[0].toLocaleUpperCase();
          authGate.hidden = true;
          chatApp.hidden = false;
          connectionStatus.textContent = 'Signed in as ' + user.username + '.';
          await refreshConnectionState();
          await refreshConversations();
        } catch (_) { authGate.hidden = false; chatApp.hidden = true; }
      }

      document.getElementById('logout-button').addEventListener('click', async function () {
        const button = this;
        const error = document.getElementById('logout-error');
        error.hidden = true; button.disabled = true;
        try {
          const response = await fetch('/api/auth/logout', { method: 'POST' });
          if (!response.ok) throw new Error('Sign out failed. Please try again.');
          window.location.reload();
        } catch (_) { error.textContent = 'Sign out failed. Please try again.'; error.hidden = false; }
        finally { button.disabled = false; }
      });

      async function loadConversation(item) {
        if (busy || loadingConversation) return;
        showSettings(false);
        loadingConversation = true;
        const version = ++conversationVersion;
        setInteractionState();
        setSidebar(false, true);
        try {
          const response = await fetch('/api/conversations/' + encodeURIComponent(item.id), { cache: 'no-store' });
          const result = await response.json();
          if (!response.ok || !Array.isArray(result.messages)) throw new Error('load');
          if (version !== conversationVersion) return;
          conversationId = item.id;
          historyError = '';
          conversationTitle.textContent = (result.conversation && result.conversation.title) || item.title || 'Conversation';
          conversation.length = 0;
          messageList.replaceChildren();
          input.value = '';
          for (const item of result.messages || []) { addMessage(item.role, item.content); conversation.push({ role: item.role, content: item.content }); }
          notice.textContent = 'This conversation is saved to your account.';
          notice.classList.remove('error');
          renderConversations();
        } catch (_) {
          if (version === conversationVersion) {
            showError('That conversation could not be loaded. Your current conversation is unchanged.', '');
            historyError = 'That conversation could not be loaded. Select it again to retry.';
            renderConversations();
          }
        } finally {
          loadingConversation = false;
          setInteractionState();
        }
      }

      function addMessage(role, content) {
        const bubble = document.createElement('article');
        bubble.className = 'message ' + role;
        const sender = document.createElement('div');
        sender.className = 'sender';
        sender.textContent = role === 'user' ? 'You' : 'Agent';
        const text = document.createElement('div');
        text.className = 'message-text';
        text.textContent = content;
        bubble.append(sender, text);
        messageList.appendChild(bubble);
        messageList.scrollTop = messageList.scrollHeight;
        return bubble;
      }

      function showError(message, action) {
        notice.replaceChildren(document.createTextNode(message));
        if (action === 'manage_usage') {
          const separator = document.createTextNode(' ');
          const link = document.createElement('a');
          link.href = 'https://chatgpt.com/settings/usage';
          link.target = '_blank';
          link.rel = 'noopener noreferrer';
          link.textContent = 'Manage usage';
          notice.append(separator, link);
        }
        notice.classList.add('error');
      }

      function updateConnectionControls() {
        const pending = connectionBusy || Boolean(connectionRefresh);
        for (const button of [connectButton, addAccountButton, disconnectButton, checkConnectionButton]) button.disabled = pending;
        accountSelect.disabled = pending || !localHost || profileCount === 0 || (profileCount === 1 && Boolean(activeProfileID));
        checkConnectionButton.textContent = connectionRefresh ? 'Checking…' : 'Check connection';
      }

      function connectionFeedback(text, warning) {
        providerFeedback.textContent = text;
        providerFeedback.hidden = !text;
        providerFeedback.classList.toggle('warning', Boolean(warning));
      }

      function refreshConnectionState() {
        if (connectionRefresh) return connectionRefresh;
        connectionRefresh = refreshConnectionData().finally(function () {
          connectionRefresh = null;
          updateConnectionControls();
        });
        updateConnectionControls();
        return connectionRefresh;
      }

      async function refreshConnectionData() {
        let connected = false;
        let oauthSelected = false;
        let planUsageEnabled = false;
        let statusAvailable = false;

        let profilesAvailable = !localHost;
        selectedProfile = null;
        activeProfileID = '';
        profileCount = 0;
        pendingProfiles = [];
        try {
          const response = await fetch('/api/auth/status', { cache: 'no-store' });
          if (!response.ok) throw new Error('status');
          const status = await response.json();
          connected = Boolean(status.connected);
          oauthSelected = Boolean(status.oauth_selected);
          planUsageEnabled = Boolean(status.plan_usage_enabled);
          statusAvailable = true;
        } catch (_) {}

        if (localHost) {
          try {
            const response = await fetch('/api/auth/profiles', { cache: 'no-store' });
            if (!response.ok) throw new Error('profiles');
            const result = await response.json();
            const profiles = Array.isArray(result.profiles) ? result.profiles : [];
            profilesAvailable = true;
            profileCount = profiles.length;
            activeProfileID = result.active_profile_id || '';
            pendingProfiles = profiles.filter(profile => profile && profile.id && profile.registration_pending);
            accountSelect.replaceChildren();
            const placeholder = document.createElement('option');
            placeholder.value = '';
            placeholder.textContent = 'Select active account…';
            accountSelect.appendChild(placeholder);
            for (const profile of profiles) {
              if (!profile || !profile.id) continue;
              const option = document.createElement('option');
              option.value = profile.id;
              option.dataset.registrationPending = profile.registration_pending ? 'true' : 'false';
              option.textContent = (profile.label || 'ChatGPT account') + (profile.email ? ' · ' + profile.email : '') + (profile.registration_pending ? ' · finish sign-in' : (profile.plan_usage_enabled ? ' · connected' : (profile.oauth_selected ? ' · sign-in required' : ' · disconnected')));
              accountSelect.appendChild(option);
              if (profile.id === activeProfileID) {
                selectedProfile = profile;
              }
            }
            accountSelect.value = result.active_profile_id || '';
            accountSelect.hidden = profiles.length === 0;

            addAccountButton.hidden = profiles.length === 0;
          } catch (_) {
            accountSelect.hidden = true;
            addAccountButton.hidden = true;
          }
        } else {
          accountSelect.hidden = true;
          addAccountButton.hidden = true;
        }

        document.getElementById('account-select-label').hidden = accountSelect.hidden;
        connectionAvailable = statusAvailable && profilesAvailable;
        const pendingProfile = selectedProfile?.registration_pending || (!activeProfileID && pendingProfiles.length > 0);
        const ready = connectionAvailable && connected && planUsageEnabled;
        connectionBadge.textContent = !connectionAvailable ? 'Unavailable' : ready ? 'Connected' : pendingProfile ? 'Awaiting sign-in' : connected ? 'Permission required' : oauthSelected ? 'Sign-in required' : 'Not connected';
        connectionBadge.className = 'connection-badge' + (ready ? '' : (!connectionAvailable || pendingProfile || connected || oauthSelected) ? ' warning' : ' off');
        connectionStatus.textContent = !connectionAvailable
          ? 'ChatGPT connection status is unavailable. Check the connection to try again.'
          : (connected && planUsageEnabled
            ? 'Using this account’s ChatGPT plan allowance.'
            : (connected
              ? 'ChatGPT sign-in is connected, but plan usage is not enabled. Review ChatGPT permissions to continue.'
              : (localHost
                ? 'ChatGPT is not connected. Continue with ChatGPT to use your ChatGPT plan.'
                : 'Your ChatGPT account is not connected. Manage accounts on the Windows host running Docker.')));
        if (pendingProfile && connectionAvailable) connectionStatus.textContent = 'Finish authorization before using an incomplete account. Select it above to resume sign-in.';
        connectButton.hidden = !localHost || ready;
        connectButton.textContent = pendingProfile ? 'Resume sign-in ↗' : connected ? 'Review ChatGPT permissions ↗' : oauthSelected ? 'Reconnect with ChatGPT ↗' : 'Continue with ChatGPT ↗';
        disconnectButton.hidden = !localHost || !oauthSelected || !selectedProfile || !connectionAvailable;
        document.getElementById('connection-note').textContent = localHost
          ? 'Adding or reconnecting an account opens ChatGPT authorization in your browser. No password is entered in Agent.'
          : 'Connection management is available only from a browser on the Windows host running Agent. Open Agent there to add, switch, or disconnect accounts.';

        try {
          const response = await fetch('/api/models', { cache: 'no-store' });
          const result = await response.json();
          if (!response.ok || !Array.isArray(result.models)) throw new Error('models');
          modelSelect.replaceChildren();
          for (const model of result.models) {
            if (!model || !model.id) continue;
            const option = document.createElement('option');
            option.value = model.id;
            option.textContent = model.name || model.id;
            modelSelect.appendChild(option);
          }
          if (!modelSelect.options.length) throw new Error('empty models');
          let preferredModel = '';
          try { preferredModel = localStorage.getItem(modelStorageKey) || ''; } catch (_) {}
          modelSelect.value = result.models.some(model => model.id === preferredModel)
            ? preferredModel
            : modelSelect.options[0].value;
          modelSelect.disabled = false;
        } catch (_) {
          modelSelect.replaceChildren(new Option('Models unavailable', ''));
          modelSelect.disabled = true;
          if (ready) connectionStatus.textContent = 'Connected to ChatGPT, but its model list could not be loaded.';
        }
      }

      checkConnectionButton.addEventListener('click', async function () {
        if (connectionBusy || connectionRefresh) return;
        providerError.hidden = true;
        connectionFeedback('');
        await refreshConnectionState();
        if (connectionAvailable) connectionFeedback('Connection status refreshed.');
        else { providerError.textContent = 'Could not check the connection. Please try again. Your saved accounts have not been removed.'; providerError.hidden = false; }
      });

      connectButton.addEventListener('click', function () {
        if (!localHost || connectionBusy || connectionRefresh) return;
        if (!activeProfileID && pendingProfiles.length > 1) {
          providerError.textContent = 'Select the incomplete ChatGPT account above to resume its sign-in.';
          providerError.hidden = false;
          accountSelect.focus();
          return;
        }
        window.location.assign('/auth/start');
      });

      addAccountButton.addEventListener('click', function () {
        if (!localHost || connectionBusy || connectionRefresh) return;
        window.location.assign('/auth/start?mode=add');
      });

      accountSelect.addEventListener('change', async function () {
        if (!localHost || connectionBusy || connectionRefresh) return;
        providerError.hidden = true;
        connectionFeedback('');
        if (!accountSelect.value) return;
        if (accountSelect.selectedOptions[0]?.dataset.registrationPending === 'true') {
          window.location.href = '/auth/start?mode=retry&profile_id=' + encodeURIComponent(accountSelect.value);
          return;
        }
        connectionBusy = true;
        updateConnectionControls();
        try {
          const response = await fetch('/api/auth/profiles/active', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ profile_id: accountSelect.value })
          });
          if (!response.ok) throw new Error('select account');
          await refreshConnectionState();
          connectionFeedback('Active ChatGPT account changed.');
        } catch (_) {
          const error = document.getElementById('provider-error');
          error.textContent = 'Your ChatGPT account selection could not be changed.';
          error.hidden = false;
          await refreshConnectionState();
        } finally {
          connectionBusy = false;
          updateConnectionControls();
        }
      });

      disconnectButton.addEventListener('click', function () {
        if (!localHost || connectionBusy || connectionRefresh || !selectedProfile) return;
        providerError.hidden = true;
        connectionFeedback('');
        disconnectProfileID = selectedProfile.id;
        document.getElementById('disconnect-account').textContent = 'Disconnect ' + (selectedProfile.label || 'ChatGPT account') + (selectedProfile.email ? ' · ' + selectedProfile.email : '') + ' from Agent on this host?';
        connectionBusy = true;
        updateConnectionControls();
        disconnectDialog.showModal();
      });
      document.getElementById('cancel-disconnect').addEventListener('click', function () { disconnectDialog.close(); });
      disconnectDialog.addEventListener('close', function () {
        if (disconnectDialog.returnValue === 'disconnect') return;
        disconnectProfileID = '';
        connectionBusy = false;
        updateConnectionControls();
        disconnectButton.focus();
      });
      document.getElementById('confirm-disconnect').addEventListener('click', async function () {
        if (!disconnectDialog.open) return;
        const profileID = disconnectProfileID;
        disconnectProfileID = '';
        disconnectDialog.close('disconnect');
        try {
          const response = await fetch('/api/auth/disconnect', {
            method: 'POST', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ profile_id: profileID })
          });
          if (!response.ok) throw new Error(response.status === 409 ? 'The active ChatGPT account changed. Review the account and try again.' : 'The ChatGPT connection could not be removed. Please try again.');
          const result = await response.json();
          await refreshConnectionState();
          connectionFeedback(result.remote_revocation_confirmed
            ? 'Disconnected from ChatGPT on this host.'
            : 'Tokens were cleared here, but remote revocation was not confirmed. Revoke this app in ChatGPT settings.', !result.remote_revocation_confirmed);
        } catch (failure) {
          const error = document.getElementById('provider-error');
          error.textContent = failure.message || 'The ChatGPT connection could not be removed. Please try again.';
          error.hidden = false;
          await refreshConnectionState();
        } finally {
          disconnectDialog.returnValue = '';
          connectionBusy = false;
          updateConnectionControls();
          if (!settingsPanel.hidden) (disconnectButton.hidden ? connectButton : disconnectButton).focus();
        }
      });

      modelSelect.addEventListener('change', function () {
        try { localStorage.setItem(modelStorageKey, modelSelect.value); } catch (_) {}
      });

      void requireSignIn();

      form.addEventListener('submit', async function (event) {
        event.preventDefault();
        const content = input.value.trim();
        if (!content || busy || loadingConversation) return;
        const version = conversationVersion;

        const welcome = document.getElementById('welcome');
        if (welcome) welcome.remove();
        addMessage('user', content);
        const requestMessages = conversation.slice(-39).concat([{ role: 'user', content: content }]);
        const thinking = addMessage('assistant', 'Thinking…');
        input.value = '';
        busy = true;
        setInteractionState();
        notice.textContent = 'Waiting for the model…';
        notice.classList.remove('error');

        try {
          const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ messages: requestMessages, model: modelSelect.value, conversation_id: conversationId })
          });
          const result = await response.json();
          if (version !== conversationVersion) return;
          if (!response.ok) {
            const requestError = new Error(result.error || 'The request failed.');
            requestError.action = result.action;
            throw requestError;
          }
          thinking.remove();
          conversationId = result.conversation_id || conversationId;
          if (conversationTitle.textContent === 'New conversation') conversationTitle.textContent = content.slice(0, 80);
          addMessage('assistant', result.reply);
          conversation.push({ role: 'user', content: content }, { role: 'assistant', content: result.reply });
          if (conversation.length > 40) conversation.splice(0, conversation.length - 40);
          notice.textContent = conversationId ? 'This conversation is saved to your account.' : 'Reply received. This conversation has not been saved.';
          await refreshConversations();
        } catch (error) {
          if (version !== conversationVersion) return;
          thinking.remove();
          addMessage('assistant', 'I could not complete that request.');
          showError(error.message || 'Check the model provider settings and try again.', error.action);
          if (error.action === 'reauthorize') await refreshConnectionState();
        } finally {
          busy = false;
          setInteractionState();
          if (settingsPanel.hidden) input.focus();
        }
      });

      input.addEventListener('keydown', function (event) {
        if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
          event.preventDefault();
          form.requestSubmit();
        }
      });

      document.getElementById('new-chat').addEventListener('click', function () {
        if (busy || loadingConversation) return;
        showSettings(false);
        conversationVersion++;
        conversation.length = 0;
        conversationId = '';
        conversationTitle.textContent = 'New conversation';
        renderConversations();
        setSidebar(false, false);
        input.value = '';
        messageList.replaceChildren();
        const welcome = document.createElement('div');
        welcome.className = 'welcome';
        welcome.id = 'welcome';
        const title = document.createElement('h1');
        title.textContent = 'What can I help with?';
        const prompt = document.createElement('p');
        prompt.textContent = 'Ask a question to start a conversation.';
        welcome.append(title, prompt);
        messageList.appendChild(welcome);
        notice.textContent = 'Messages are saved to your account after each reply.';
        notice.classList.remove('error');
        input.focus();
      });
    })();
  </script>
</body>
</html>`
