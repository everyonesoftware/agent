package main

const homePage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="dark">
  <meta name="app-version" content="{{APP_VERSION}}">
  <meta name="app-build-id" content="{{APP_BUILD_ID}}">
  <meta name="theme-color" content="#202322">
  <meta name="apple-mobile-web-app-capable" content="yes">
  <meta name="apple-mobile-web-app-title" content="Agent">
  <link rel="manifest" href="/manifest.webmanifest">
  <link rel="apple-touch-icon" href="/icons/apple-touch-icon.png">
  <script src="/pwa-updates.js" defer></script>
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
    button:focus-visible, input:focus-visible, select:focus-visible, a:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
    button:disabled { opacity: .5; cursor: wait; }
    a { color: var(--accent); }
    .brand { display: flex; align-items: center; gap: 9px; color: #e1e6e2; font-size: 15px; font-weight: 600; letter-spacing: -.4px; }
    .mark { display: grid; place-items: center; width: 26px; height: 26px; flex-shrink: 0; border-radius: 6px; color: #1c281a; background: var(--accent); font: 17px Georgia, serif; letter-spacing: -1.2px; padding-right: 1px; }
    .app { position: relative; display: grid; grid-template-columns: clamp(180px, var(--sidebar-width, 232px), min(480px, 45vw)) minmax(0, 1fr); width: 100%; height: 100vh; height: 100dvh; margin: 0; border: 0; background: #202322; }
    .sidebar { position: relative; min-width: 0; display: flex; flex-direction: column; gap: 18px; min-height: 0; padding: 25px 14px 12px; border-right: 1px solid var(--line); background: #181b1a; }
    .sidebar-resize { position: absolute; top: 0; right: -4px; bottom: 0; width: 8px; z-index: 5; cursor: col-resize; touch-action: none; }
    .sidebar-resize:hover, .sidebar-resize:focus-visible { background: #b4d6a355; }
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
    .session-title { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; overflow: hidden; }
    .session[aria-current="true"] { color: var(--accent); background: #2c3930; }
    .session small { display: block; color: var(--muted); font-size: 10px; margin-top: 3px; }
    .history-state { color: var(--muted); font-size: 12px; padding: 6px 9px; }
    .history-state.error { color: #efada5; }
    .account-area { position: relative; border-top: 1px solid var(--line); padding-top: 10px; }
    .account-row { display: flex; align-items: center; gap: 4px; }
    .app-version { display: block; padding: 2px 6px 0; color: var(--muted); font-size: 10px; overflow-wrap: anywhere; }
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
    .settings-panel { position: relative; flex: 1; min-height: 0; overflow-y: auto; scrollbar-gutter: stable; padding: 30px; }
    .settings-content { max-width: 600px; margin: 0 auto; }
    .settings-content > p, .settings-card > p { color: var(--muted); font-size: 12px; }
    .settings-card { margin-top: 24px; padding: 22px; border: 1px solid var(--line); border-radius: 8px; background: var(--field); }
    .settings-card h2 { margin: 0 0 6px; font-size: 16px; font-weight: 500; }
    .settings-card label { display: block; margin: 16px 0 6px; font-size: 12px; }
    .settings-card input { display: block; width: 100%; padding: 10px 12px; color: inherit; background: #202322; border: 1px solid var(--line); border-radius: 5px; }
    .settings-card .send { margin: 16px 0 0; }
    .connection-heading { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px; margin-bottom: 6px; }
    .connection-heading h2 { margin: 0; }
    .connection-refresh { display: inline-flex; align-items: center; gap: 7px; min-height: 32px; padding: 4px 10px; border: 1px solid #45513f; border-radius: 20px; color: var(--accent); }
    .connection-refresh:has(.off) { border-color: var(--line); color: var(--muted); }
    .connection-refresh:has(.warning) { border-color: #5c513b; color: #dfc38e; }
    .connection-refresh:hover:not(:disabled) { border-color: #5b6a5f; background: #2c3930; }
    .connection-refresh svg { width: 13px; height: 13px; flex-shrink: 0; }
    .connection-refresh[aria-busy="true"] svg { animation: connection-refresh 1s linear infinite; }
    @keyframes connection-refresh { to { transform: rotate(360deg); } }
    @media (prefers-reduced-motion: reduce) { .connection-refresh[aria-busy="true"] svg { animation: none; } }
    @media (max-width: 480px) { .connection-refresh { min-height: 44px; } }
    .connection-badge { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; font-size: 11px; color: var(--accent); }
    .connection-badge::before { content: ''; width: 5px; height: 5px; border-radius: 50%; background: currentColor; }
    .connection-badge.warning { color: #dfc38e; }
    .connection-badge.off { color: var(--muted); }
    .connection-setup { margin-top: 14px; font-size: 12px; }
    .connection-setup .setup-lead { margin: 0 0 4px; font-size: 14px; font-weight: 500; }
    .connection-setup p { margin: 8px 0; }
    .connection-setup .setup-intro, .connection-setup li > span, .connection-setup .setup-return { color: var(--muted); }
    .connection-setup ol { list-style: none; padding: 0; margin: 20px 0; counter-reset: setup-step; }
    .connection-setup li { position: relative; padding-left: 31px; margin: 15px 0; counter-increment: setup-step; }
    .connection-setup li::before { content: counter(setup-step); position: absolute; left: 0; top: 0; width: 21px; height: 21px; border: 1px solid #475249; border-radius: 50%; color: var(--accent); font-size: 11px; text-align: center; line-height: 19px; }
    .connection-setup strong { font-weight: 500; }
    .connection-setup li > span { display: block; margin-top: 2px; }
    .connection-setup .setup-return { border-top: 1px solid var(--line); padding-top: 15px; }
    .connection-help { margin-top: 17px; font-size: 12px; color: var(--muted); }
    .connection-help summary { cursor: pointer; width: fit-content; padding: 4px 0; }
    .connection-help p { margin: 8px 0 0; }
    .settings-card #account-select { display: block; width: 100%; max-width: 100%; min-height: 40px; margin-bottom: 14px; font-size: 12px; background: #202322; }
    .settings-card .connection-status { margin: 14px 0 3px; color: inherit; font-size: 12px; }
    .connection-detail { margin: 0; color: var(--muted); font-size: 11px; }
    .local-connection-address { display: flex; align-items: center; gap: 12px; margin-top: 14px; padding: 8px 10px; border: 1px solid var(--line); border-radius: 5px; background: #202322; }
    .local-connection-address > div { flex: 1; min-width: 0; }
    .local-connection-address code { display: block; overflow-wrap: anywhere; font-size: 12px; user-select: all; }
    .local-connection-address button { min-height: 44px; flex-shrink: 0; }
    #local-connection-config-error { margin-top: 12px; color: #dfc38e; }
    .settings-card .provider-actions { margin-top: 18px; gap: 8px; align-items: center; }
    .settings-card .provider-actions .send { margin: 0; }
    .danger { color: #efada5; }
    .provider-actions .danger { margin-left: auto; background: transparent; }
    .settings-card .provider-actions.with-guidance { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 0 12px; align-items: start; }
    .with-guidance > .connection-help { display: contents; }
    .with-guidance > .connection-help::details-content { display: contents; }
    .with-guidance > .connection-help > summary { grid-column: 1; grid-row: 1; display: flex; align-items: center; gap: 7px; width: 100%; min-height: 44px; padding: 9px 0; list-style: none; }
    .with-guidance > .connection-help > summary::-webkit-details-marker { display: none; }
    .with-guidance > .connection-help > summary::before { content: '›'; font-size: 17px; line-height: 1; flex: none; }
    .with-guidance > .connection-help[open] > summary::before { transform: rotate(90deg); }
    .with-guidance > .connection-help > summary:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
    .with-guidance > .connection-help > div { grid-column: 1 / -1; grid-row: 2; padding-top: 6px; }
    .with-guidance > .connection-help:not([open]) > div { display: none; }
    .with-guidance > .danger { grid-column: 2; grid-row: 1; min-height: 44px; }

    #provider-feedback { margin: 14px 0 0; color: var(--accent); font-size: 12px; }
    #provider-feedback.warning { color: #dfc38e; }
    #research-settings-card { overflow-wrap: anywhere; }
    #research-settings-card button, #research-key-remove-dialog button { min-height: 44px; }
    .research-key-field { position: relative; display: flex; min-width: 0; }
    .research-key-field input { min-width: 0; min-height: 44px; padding-right: 66px; }
    #research-key-show { position: absolute; right: 2px; top: 0; padding: 0 10px; color: var(--muted); }
    .research-settings-actions { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-top: 16px; }
    #research-settings-card .send { margin: 0; }
    .research-settings-notice { margin-top: 16px; padding: 12px; border: 1px solid #3c493e; border-radius: 5px; background: #232d26; color: var(--muted); font-size: 12px; }
    .research-saved { margin-top: 18px; padding: 12px; border: 1px solid var(--line); border-radius: 5px; background: #202322; }
    #research-key-suffix { font: 13px Consolas, monospace; }
    #research-key-error:empty, #research-key-feedback:empty { display: none; }
    #research-settings-card .connection-help summary { min-height: 44px; }
    .security-toggle { display: flex; align-items: center; justify-content: space-between; width: 100%; padding: 0; text-align: left; }
    .security-toggle h2 { margin: 0; }
    .security-copy { margin: 7px 0 0; }
    .connection-dialog { width: 420px; max-width: calc(100% - 32px); max-height: calc(100dvh - 32px); overflow-y: auto; padding: 24px; border: 1px solid #465049; border-radius: 8px; background: var(--field); color: #e1e6e2; }
    .connection-dialog::backdrop { background: #0009; }
    .connection-dialog h2 { margin: 0 0 12px; font-size: 16px; font-weight: 500; }
    .connection-dialog p { font-size: 12px; color: var(--muted); overflow-wrap: anywhere; }
    .connection-dialog label { display: block; margin: 16px 0 6px; font-size: 12px; }
    .connection-dialog input { display: block; width: 100%; min-height: 44px; padding: 10px 12px; color: inherit; background: #202322; border: 1px solid var(--line); border-radius: 5px; }
    .connection-dialog .menu-error { color: #efada5; }
    .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
    #password-status { margin: 12px 0 0; font-size: 12px; color: var(--accent); }
    #password-status.error { color: #efada5; }
    #settings-title { margin: 0; font-size: 13px; font-weight: 500; }
    .workspace { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
    .workspace-header { min-height: 65px; padding: 12px 30px; border-bottom: 1px solid var(--line); display: flex; justify-content: flex-start; gap: 12px; }
    #conversation-title { margin: 0; font-size: 13px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    #conversation-save-status { flex: none; margin-left: auto; white-space: nowrap; color: var(--muted); font-size: 11px; }
    #conversation-save-status::before { content: '✓'; color: var(--accent); margin-right: 6px; }
    .sidebar-toggle, .sidebar-backdrop { display: none; }
    .messages { position: relative; flex: 1; min-height: 0; overflow-y: auto; padding: 34px max(24px, calc((100% - 740px) / 2)); scroll-behavior: auto; }
    .welcome { max-width: 540px; margin: clamp(25px, 12vh, 130px) auto 0; text-align: left; }
    .welcome h1 { margin: 0 0 8px; font-size: clamp(24px, 3vw, 32px); font-weight: 450; letter-spacing: -.8px; }
    .welcome p { color: var(--muted); font-size: 13px; }
    .message { width: 100%; max-width: 740px; min-width: 0; padding: 0; margin: 0 auto 30px; border: 0; border-radius: 0; background: transparent; }
    .message.user { width: fit-content; max-width: 86%; margin-right: 0; margin-left: auto; }
    .sender { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; font-size: 11px; color: var(--muted); font-weight: 500; text-transform: none; letter-spacing: 0; }
    .message.user .sender { justify-content: flex-end; margin-bottom: 7px; }
    .message.user .message-text { padding: 12px 17px; color: #e1e8df; background: #2c3930; border: 1px solid #394b3d; border-radius: 12px 12px 3px 12px; }
    .message.assistant .sender { color: #e1e6e2; font-weight: 600; }
    .message.assistant .sender::before { content: 'e1'; display: grid; place-items: center; width: 21px; height: 21px; flex: none; border-radius: 5px; background: #2e3a30; color: var(--accent); font: 13px Georgia, serif; letter-spacing: -1px; }
    .message-text { font-size: 14px; line-height: 1.75; white-space: pre-wrap; overflow-wrap: anywhere; }
    .message.assistant .message-text { white-space: normal; }
    .message-text p { margin: 0 0 15px; white-space: pre-wrap; }
    .message-text h2, .message-text h3, .message-text h4 { margin: 23px 0 9px; font-size: 15px; line-height: 1.5; font-weight: 600; letter-spacing: -.15px; }
    .message-text h3, .message-text h4 { font-size: 14px; }
    .message-text ul, .message-text ol { margin: 0 0 17px; padding-left: 21px; }
    .message-text li { margin: 7px 0; padding-left: 3px; white-space: pre-wrap; }
    .message-text strong { color: #eef2ee; font-weight: 600; }
    .message-text code { font: 12px/1.65 Consolas, monospace; background: #181b1a; padding: 2px 5px; border-radius: 4px; }
    .message-text pre { max-width: 100%; overflow-x: auto; padding: 14px 16px; margin: 16px 0; border: 1px solid var(--line); border-radius: 6px; background: #181b1a; white-space: pre; overflow-wrap: normal; }
    .message-text pre code { padding: 0; border-radius: 0; }
    .message-text blockquote { margin: 16px 0; padding: 0 16px; border-left: 2px solid #56645a; color: var(--muted); white-space: pre-wrap; }
    .message-text > :first-child { margin-top: 0; }
    .message-text > :last-child { margin-bottom: 0; }
    .research-opt-in { display: inline-flex; align-items: center; gap: 7px; font-size: 12px; color: var(--muted); min-height: 32px; }
    .research-opt-in input { accent-color: var(--accent); width: 16px; height: 16px; margin: 0; }
    .research-card { display: flex; flex-direction: column; padding: 14px; margin-bottom: 10px; border: 1px solid #596b50; border-radius: 8px; background: #272e28; max-height: min(55dvh, 430px); overflow: hidden; overflow-wrap: anywhere; }
    .research-consent-copy { overflow: auto; min-height: 0; }
    .research-card > .research-actions { flex-shrink: 0; padding-top: 8px; }
    .research-card h2 { font-size: 15px; margin: 0 0 8px; }
    .research-card p { margin: 8px 0; font-size: 12px; line-height: 1.5; }
    .research-card pre { white-space: pre-wrap; overflow-wrap: anywhere; font: inherit; font-size: 12px; }
    .research-actions { display: flex; flex-wrap: wrap; gap: 8px; }
    .research-actions button { min-height: 44px; white-space: normal; }
    #research-approve { background: #35442e; color: #d1e7c5; }
    #research-scope { margin-bottom: 8px; border: 1px solid var(--line); border-radius: 8px; padding: 8px 10px; background: #252c26; max-height: 28dvh; overflow: auto; }
    .research-badge { align-self: center; color: #c1d7b6; font-size: 12px; margin-right: auto; }
    #research-permission-details p, #research-control-feedback { font-size: 12px; margin: 8px 0; overflow-wrap: anywhere; }
    #research-control-feedback:empty { display: none; }
    #research-consent:not([hidden]) ~ .hint { display: none; }
    .research-card[hidden], #research-scope[hidden] { display: none; }
    .composer-wrap { width: 100%; max-width: 796px; margin: 0 auto; padding: 10px 28px 18px; border: 0; }
    .notice { min-height: 18px; color: var(--muted); font-size: 11px; margin: 0 0 8px; }
    .notice.error { color: #efada5; }
    .composer { position: relative; display: flex; flex-wrap: wrap; align-items: center; gap: 10px; padding: 12px; border: 1px solid var(--line); border-radius: 8px; background: var(--field); box-shadow: none; }
    textarea { width: 100%; flex: 1 0 100%; min-width: 0; min-height: 42px; max-height: min(400px, 45dvh); padding: 3px 32px 3px 3px; border: 0; resize: none; outline: none; color: inherit; background: transparent; }
    .composer-resize { position: absolute; top: 0; right: 0; display: flex; align-items: flex-start; justify-content: flex-end; width: 44px; height: 44px; padding: 0; color: #7f8b82; cursor: nesw-resize; touch-action: none; }
    .composer-resize svg { margin: 8px 8px 0 0; }
    .composer-resize:hover, .composer-resize:focus-visible { color: var(--accent); }
    textarea::placeholder { color: var(--muted); }
    .model-select { min-width: 0; max-width: min(70%, 300px); padding: 6px 8px; border: 1px solid var(--line); border-radius: 5px; background: var(--field); color: #c5cec7; font-size: 11px; }
    .send { margin-left: auto; min-width: 54px; padding: 7px 12px; border-radius: 5px; background: var(--accent); color: #1c281a; font-size: 12px; font-weight: 600; }
    .send:hover:not(:disabled) { background: #c4e5b5; }
    .hint { margin: 7px 0 0; color: var(--muted); font-size: 10px; text-align: center; }
    .chat-layout { display: flex; position: relative; flex: 1; min-height: 0; min-width: 0; }
    .chat-thread { display: flex; flex-direction: column; flex: 1; min-width: 0; min-height: 0; }
    .inspect-run { display: inline-flex; align-items: center; gap: 6px; margin-top: 10px; padding: 5px 0; min-height: 32px; font-size: 11px; color: var(--accent); }
    .inspect-run:hover { text-decoration: underline; }
    .inspect-run svg { width: 14px; height: 14px; flex: none; }
    .run-inspector { display: flex; flex-direction: column; flex: 0 0 430px; max-width: 44%; min-width: 0; min-height: 0; background: #1b1f1c; border-left: 1px solid #3c453f; }
    .run-heading { display: flex; justify-content: space-between; gap: 8px; align-items: center; padding: 12px 17px 5px; }
    .run-heading > div { display: flex; align-items: center; flex-wrap: wrap; gap: 9px; }
    .run-heading h2 { font-size: 13px; font-weight: 600; margin: 0; }
    .run-state { display: inline-flex; align-items: center; gap: 5px; color: var(--muted); font-size: 10px; }
    .run-state::before { content: ''; width: 5px; height: 5px; border-radius: 50%; background: currentColor; }
    .run-state.completed { color: var(--accent); }
    .run-state.failed, .run-state.interrupted { color: #efada5; }
    .run-state.running { color: #dfc38e; }
    .run-close { min-width: 36px; min-height: 36px; font-size: 20px; line-height: 1; padding: 0; color: var(--muted); }
    .run-meta { padding: 0 17px 11px; color: var(--muted); font-size: 10px; overflow-wrap: anywhere; }
    .run-tabs { display: flex; gap: 20px; padding: 0 17px; border-bottom: 1px solid var(--line); }
    .run-tabs button { min-height: 40px; border-radius: 0; border-bottom: 2px solid transparent; padding: 6px 0; color: var(--muted); font-size: 12px; }
    .run-tabs button[aria-selected="true"] { color: var(--accent); border-bottom-color: var(--accent); }
    .run-scroll { flex: 1; min-height: 0; overflow-y: auto; padding: 17px; position: relative; }
    .run-caption { margin: 0 0 15px; font-size: 11px; color: var(--muted); }
    .run-feedback { margin-bottom: 15px; color: #dfc38e; font-size: 12px; }
    .run-feedback p { margin: 0 0 7px; }
    .run-inspector dl { margin: 0 0 18px; }
    .run-inspector dl > div { display: grid; grid-template-columns: 112px minmax(0, 1fr); gap: 12px; padding: 7px 0; border-bottom: 1px solid #2c332e; font-size: 12px; }
    .run-inspector dt { color: var(--muted); }
    .run-inspector dd { margin: 0; overflow-wrap: anywhere; }
    .run-inspector h3 { margin: 17px 0 9px; font-size: 11px; font-weight: 600; }
    .run-block { padding: 12px; border: 1px solid var(--line); border-radius: 5px; margin: 0 0 12px; background: #202522; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 12px; }
    .run-inspector pre { font: 11px/1.65 Consolas, monospace; white-space: pre-wrap; overflow-wrap: anywhere; margin: 0 0 15px; padding: 12px; border: 1px solid var(--line); border-radius: 5px; background: #151916; }
    .run-inspector summary { min-height: 36px; cursor: pointer; color: var(--accent); font-size: 12px; padding: 6px 0; }
    .run-inspector summary:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
    .run-event { position: relative; display: grid; grid-template-columns: 22px minmax(0, 1fr); gap: 9px; padding-bottom: 19px; }
    .run-event:not(:last-child)::before { content: ''; position: absolute; left: 10px; width: 1px; top: 23px; bottom: 3px; background: #3a453c; }
    .run-event-mark { display: grid; place-items: center; width: 21px; height: 21px; border: 1px solid #465543; border-radius: 50%; color: var(--accent); font-size: 11px; }
    .run-event.failed .run-event-mark { color: #efada5; border-color: #78514b; }
    .run-event.running .run-event-mark { color: #dfc38e; border-color: #665d44; }
    .run-event-title { font-size: 12px; font-weight: 500; }
    .run-event-detail, .run-event-time { color: var(--muted); font-size: 11px; overflow-wrap: anywhere; }
    .run-event-time { margin-top: 3px; font-size: 10px; }
    .run-event button { min-height: 30px; padding: 3px 0; color: var(--accent); font-size: 11px; }
    .run-footer { padding: 10px 17px; border-top: 1px solid var(--line); color: var(--muted); font-size: 10px; }
    @media (max-width: 1000px) {
      .run-inspector { position: absolute; inset: 0; z-index: 8; max-width: none; width: 100%; border-left: 0; }
    }
    @media (max-width: 700px) {
      .run-close, .run-tabs button, .run-inspector summary, .inspect-run, .run-event button { min-height: 44px; }
      .run-close { min-width: 44px; }
      .run-heading { padding: 8px 14px 4px; }
      .run-scroll { padding: 14px; }
      .run-tabs { padding: 0 14px; }
      .run-inspector dl > div { grid-template-columns: 90px minmax(0, 1fr); gap: 10px; font-size: 11px; }
    }
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
      .sidebar-resize { display: none; }
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
    .usage-subsection { border-top:1px solid var(--line); padding-top:20px; margin-top:16px; font-size:12px; overflow-wrap:anywhere; }
    .usage-heading { display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:12px; }
    .usage-heading h3 { margin:0; font-size:14px; font-weight:500; }
    .usage-subsection p, .usage-help { color:var(--muted); font-size:11px; }
    .usage-identity { font-size:12px!important; color:inherit!important; }
    .usage-footer, .usage-actions { display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:12px; margin-top:14px; }
    #disconnect-chatgpt, .usage-subsection button, .usage-subsection a, #usage-dialog button { min-height:44px; }
    .usage-footer .danger { background:transparent; }
    .usage-help { margin-top:12px; }
    .usage-help summary { cursor:pointer; padding:6px 0; }
    .quota-window { margin:18px 0; }
    .quota-label { display:flex; justify-content:space-between; flex-wrap:wrap; gap:8px; }
    .quota-label strong { font-size:14px; font-weight:500; color:var(--accent); }
    .quota-track { height:6px; border-radius:3px; background:#39403b; margin:8px 0 5px; overflow:hidden; }
    .quota-fill { height:100%; background:var(--accent); }
    .quota-reset, #usage-updated { font-size:10px; color:var(--muted); }
    .usage-stale .quota-fill { background:#7b877d; }
    .device-panel { margin:14px 0; padding:14px; border:1px solid var(--line); border-radius:5px; background:#202322; }
    .device-code { display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:8px; }
    .device-code code { font:18px/1.5 Consolas,monospace; letter-spacing:1px; user-select:all; }
    #usage-verification { display:inline-flex; align-items:center; }
    #usage-error, #usage-confirm-error { color:#efada5; }
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
      <div class="sidebar-resize" id="sidebar-resize" role="separator" tabindex="0" aria-label="Resize sidebar" aria-orientation="vertical" aria-controls="session-sidebar" aria-valuemin="180" aria-valuemax="480" aria-valuenow="232"></div>
      <div class="brand"><span class="mark" aria-hidden="true">e1</span>Agent</div>
      <button class="reset new-chat" id="new-chat" type="button">＋ New conversation</button>
      <label class="search-label"><span class="visually-hidden">Search conversations</span><input type="search" id="conversation-search" placeholder="Search conversations" autocomplete="off"></label>
      <nav class="history" id="conversation-list" aria-label="Previous sessions"><p class="history-state">Loading conversations…</p></nav>
      <div class="account-area">
      <div class="account-row"><button class="account-toggle" id="account-menu-toggle" type="button" aria-expanded="false" aria-controls="account-menu"><span class="avatar" id="user-avatar" aria-hidden="true">?</span><span><strong id="current-username"></strong><small>Your account</small></span></button>
      <button class="settings-button" id="settings-button" type="button" aria-label="Settings" title="Settings" aria-controls="settings-panel" aria-pressed="false"><svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 3-.6 2.2-1.5.9-2.2-.6-3 5.2 1.6 1.6v1.8l-1.6 1.6 3 5.2 2.2-.6 1.5.9L9 23h6l.6-2.2 1.5-.9 2.2.6 3-5.2-1.6-1.6v-1.8l1.6-1.6-3-5.2-2.2.6-1.5-.9L15 3Z" transform="translate(1 0) scale(.92)"/><circle cx="12" cy="12" r="3.2"/></svg></button></div>
      <small class="app-version" id="app-version">Version {{APP_VERSION}}</small>
      <section class="account-menu" id="account-menu" aria-label="Account settings" hidden>
        <h2>Your account</h2>
        <button class="reset" id="manage-connections" type="button">Manage connections &amp; settings</button>
      </section></div>
    </aside>
    <div class="workspace">
    <header class="workspace-header"><button id="sidebar-toggle" class="sidebar-toggle" type="button" aria-label="Open session navigation" aria-controls="session-sidebar" aria-expanded="false">☰</button><h1 id="conversation-title">New conversation</h1><h1 id="settings-title" hidden>Settings</h1><span id="conversation-save-status" role="status" hidden>Saved</span></header>
    <section class="settings-panel" id="settings-panel" aria-labelledby="settings-title" hidden>
      <div class="settings-content">
        <button class="reset" id="back-to-chat" type="button">← Back to conversation</button>
        <p>Manage your connections, account, and security.</p>
        <section class="settings-card" id="pwa-updates" aria-labelledby="pwa-title">
          <h2 id="pwa-title">Web app updates</h2>
          <p id="pwa-version">Current version: {{APP_VERSION}}</p>
          <p id="pwa-status" role="status" aria-live="polite">Updates are applied only when you choose to reload.</p>
          <div class="provider-actions">
            <button class="reset" id="pwa-check" type="button">Check for updates</button>
            <button class="send" id="pwa-apply" type="button" hidden>Update and reload</button>
            <button class="reset" id="pwa-later" type="button" hidden>Later</button>
          </div>
        </section>
        <section class="settings-card" aria-labelledby="connection-title">
          <div class="connection-heading"><h2 id="connection-title">ChatGPT connection</h2><button class="connection-refresh" id="check-chatgpt" type="button" title="Refresh connection status" aria-label="Refresh ChatGPT connection status" aria-busy="true"><span class="connection-badge off" id="connection-badge">Checking…</span><svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M20 7v5h-5M4 17v-5h5"/><path d="M6.1 6.1A8 8 0 0 1 20 12M4 12a8 8 0 0 0 13.9 5.9"/></svg></button></div>
          <label id="account-select-label" for="account-select" hidden>Active ChatGPT account</label>
          <select class="model-select" id="account-select" hidden disabled><option value="">Loading accounts…</option></select>
          <p class="connection-status" id="connection-status" role="status">Checking ChatGPT connection…</p>
          <div id="remote-connection-setup-slot">
            <div class="connection-setup" id="remote-connection-setup" hidden>
              <p class="setup-lead" id="remote-connection-heading">Connect from the computer running Agent</p>
              <p class="setup-intro" id="remote-connection-intro">You’re viewing Agent remotely. ChatGPT sign-in must be completed in a browser on the computer hosting Agent.</p>
              <ol>
                <li><strong>On that computer, open Agent’s local address.</strong>
                  <div class="local-connection-address" id="local-connection-address" hidden>
                    <div><span class="connection-detail">Configured local address</span><code id="local-connection-url"></code></div>
                    <button class="reset" id="copy-local-connection-url" type="button" aria-label="Copy Agent’s configured local address">Copy</button>
                  </div>
                </li>
                <li><strong>Sign in to the same Agent account.</strong><span id="remote-connection-action">Open Settings, then choose Continue with ChatGPT.</span></li>
                <li><strong>Approve the connection in ChatGPT.</strong><span>Then return here and select the status pill above to refresh.</span></li>
              </ol>
              <p class="setup-return">Once connected, you can use Agent from this address. You only need localhost again to add or reconnect a ChatGPT account.</p>
              <details class="connection-help"><summary>Why can’t I connect from here?</summary><p>ChatGPT returns sign-in to a local address on the computer running your browser. On another device, that points to that device—not your Agent server. Open the address above on the computer running Agent, not on another device. If the server has no browser, use a localhost tunnel from a trusted computer for setup.</p></details>
            </div>
          </div>
          <p class="connection-detail" id="local-connection-config-error" hidden>Local connection address unavailable. Ask the server operator to set SIWC_REDIRECT_URI to a valid loopback callback matching the published host port.</p>

          <div class="provider-actions" id="provider-actions">
            <details class="connection-help" id="remote-connection-help" hidden><summary>Add or reconnect an account</summary><div id="remote-connection-help-slot"></div></details>
            <button class="send" id="connect-chatgpt" type="button" hidden>Continue with ChatGPT ↗</button>
            <button class="reset" id="add-chatgpt-account" type="button" hidden>Add account ↗</button>

            <button class="reset danger" id="disconnect-chatgpt" type="button" hidden>Disconnect</button>
          </div>
          <p id="provider-error" class="menu-error" role="alert" hidden></p>
          <p id="provider-feedback" role="status" aria-live="polite" hidden></p>
          <section class="usage-subsection" id="usage-panel" aria-labelledby="usage-heading" hidden>
            <div class="usage-heading"><h3 id="usage-heading">Allowance</h3><button id="usage-refresh" class="connection-refresh" type="button" aria-label="Refresh usage" title="Refresh usage"><span id="usage-badge" class="connection-badge off">Optional</span><svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><path d="M20 7v5h-5M4 17v-5h5M6.1 6.1A8 8 0 0 1 20 12M4 12a8 8 0 0 0 13.9 5.9"/></svg></button></div>
            <p id="usage-message">See remaining allowance and reset times with a separate Codex sign-in. Your conversation connection won’t change.</p>
            <p class="usage-identity" id="usage-identity" hidden></p>
            <div class="device-panel" id="usage-device" hidden><strong>Approve your usage connection</strong><p>Open the OpenAI verification page, sign in to the intended ChatGPT account and workspace, and enter this code. Return here after approval.</p><div class="device-code"><code id="usage-code"></code><button class="reset" id="usage-copy" type="button">Copy code</button></div><div class="usage-actions"><a id="usage-verification" target="_blank" rel="noopener noreferrer" hidden>Open verification page ↗</a><button class="reset" id="usage-cancel" type="button">Cancel</button></div><p id="usage-expiry"></p></div>
            <div id="usage-windows"></div>
            <p id="usage-error" role="alert" hidden></p><p id="usage-feedback" role="status"></p>
            <div class="usage-footer"><span id="usage-updated"></span><button class="reset" id="usage-connect" type="button">Connect usage account</button><button class="reset danger" id="usage-disconnect" type="button" aria-label="Disconnect usage connection" title="Disconnect usage connection" hidden>Disconnect</button></div>
            <details class="usage-help"><summary>About this usage connection</summary><p>This separate Codex sign-in reads account and quota details only. It does not change conversation routing or the existing ChatGPT connection. Device sign-in can start here, including remotely.</p><p>Codex-reported windows may not cover every ChatGPT feature or Agent-specific limit. Matching email does not verify account or workspace identity. Use the intended account and workspace. This connection is optional; conversations work without it.</p></details>
          </section>


        </section>
        <section class="settings-card" id="research-settings-card" aria-labelledby="research-settings-title">
          <div class="connection-heading"><h2 id="research-settings-title">Web research</h2><span class="connection-badge off" id="research-settings-badge" role="status">Not loaded</span></div>
          <p>Search public web sources with your own Brave Search key. Public-page retrieval and your ChatGPT connection work independently.</p>
          <p class="connection-detail">Provider: Brave Search · Saved for your Agent account only</p>
          <div id="research-key-saved" class="research-saved" hidden><strong id="research-key-suffix"></strong><p id="research-key-checked" class="connection-detail"></p><div class="research-settings-actions"><button class="reset" id="research-key-replace" type="button">Replace key</button><button class="reset" id="research-key-check" type="button">Test connection</button><button class="reset danger" id="research-key-remove" type="button">Remove key</button></div></div>
          <form id="research-key-form" hidden autocomplete="off">
            <label for="research-api-key">Brave Search API key</label>
            <div class="research-key-field"><input id="research-api-key" type="password" autocomplete="off" autocapitalize="off" spellcheck="false" aria-describedby="research-key-disclosure research-key-error"><button id="research-key-show" type="button" aria-pressed="false">Show</button></div>
            <p id="research-key-disclosure" class="research-settings-notice">Verify &amp; save sends one verification test query to Brave Search. This may count toward your Brave usage. Test connection also sends one test query.</p>
            <div class="research-settings-actions"><button class="send" id="research-key-verify" type="submit">Verify &amp; save</button><button class="reset" id="research-key-cancel" type="button">Cancel</button></div>
          </form>
          <p id="research-key-error" class="menu-error" role="alert"></p><p id="research-key-feedback" class="connection-detail" role="status" aria-live="polite"></p><button class="reset" id="research-settings-retry" type="button" hidden>Retry settings</button>
          <p id="research-key-consent" class="connection-detail">Saving a key does not approve research. Each task still needs your permission.</p>
          <details class="connection-help"><summary>About your search key</summary><p>Get a Search API key from your Brave Search account. Agent stores it on this server for your user; it is never sent in model messages. Only its suffix is shown after saving.</p><p>Updating or removing your key stops your active research permissions; other users are unaffected.</p></details>
        </section>
        <dialog id="research-key-remove-dialog" class="connection-dialog" aria-labelledby="research-key-remove-title"><h2 id="research-key-remove-title">Remove your Brave Search key?</h2><p>This removes the key from your Agent account and stops your active research permissions. It does not revoke the key at Brave. Public-page retrieval and your ChatGPT connection are unaffected.</p><div class="dialog-actions"><button id="research-key-keep" class="reset" type="button">Keep key</button><button id="research-key-remove-confirm" class="reset danger" type="button">Remove Brave key</button></div></dialog>
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
    <div class="chat-layout" id="chat-layout">
    <div class="chat-thread" id="chat-thread">
    <section class="messages" id="messages" aria-live="polite" aria-relevant="additions text" aria-label="Conversation">
      <div class="welcome" id="welcome">
        <h1>What can I help with?</h1>
        <p>Ask a question to start a conversation.</p>
      </div>
    </section>
    <div class="composer-wrap">
      <p class="notice" id="notice" role="status">Messages and replies are saved to your account after each successful request.</p>
      <section id="research-scope" aria-label="Active research permissions" hidden>
        <div class="research-actions"><span class="research-badge">Public web only · this task</span><button type="button" class="reset" id="research-stop">Stop</button><button type="button" class="reset" id="research-permissions" aria-expanded="false" aria-controls="research-permission-details">Permissions</button></div>
        <div id="research-permission-details" hidden><p>Only public web search and page retrieval for this task. No files, accounts, messages, or memory writes. Revoking prevents further requests; it cannot undo previous requests or recall information already sent.</p><button type="button" class="reset danger" id="research-revoke">Revoke permission</button></div>
        <p id="research-control-feedback" role="status" aria-live="polite"></p>
      </section>
      <section class="research-card" id="research-consent" aria-labelledby="research-consent-title" hidden>
        <div class="research-consent-copy" tabindex="0" role="region" aria-label="Research permission disclosure">
        <h2 id="research-consent-title">Allow public web research for this task?</h2>
        <p>One task only. Searches and page requests leave this server. Query content may include your current task prompt. The research model receives only the final prompt below, not earlier chat.</p>
        <p id="research-provider"></p>
        <p>No files, accounts, messages, or memory writes are allowed. Web content is untrusted; it cannot grant more permissions.</p>
        <details><summary>Task prompt preview</summary><pre id="research-prompt"></pre></details>
        <p id="research-feedback" role="status" aria-live="polite"></p>
        </div>
        <div class="research-actions"><button type="button" class="reset" id="research-approve" disabled>Allow for this task</button><button type="button" class="reset" id="research-decline">Answer without browsing</button><button type="button" class="reset" id="research-cancel">Cancel</button><button type="button" class="reset" id="research-retry" hidden>Retry permission request</button></div>
      </section>
      <form class="composer" id="chat-form" autocomplete="off">
        <label class="visually-hidden" for="message-input">Message</label>
        <textarea id="message-input" name="message" rows="1" maxlength="8192" placeholder="Message Agent" required></textarea>
        <button class="composer-resize" id="composer-resize" type="button" aria-label="Resize message box" title="Drag upward to expand; use Up and Down arrow keys" aria-controls="message-input"><svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" aria-hidden="true"><path d="M3.5 3.5l9 9M7.5 3.5l5 5M11.5 3.5l1 1"/></svg></button>
        <label class="visually-hidden" for="model-select">Model</label>
        <select class="model-select" id="model-select" disabled><option value="">Loading models…</option></select>
        <label class="research-opt-in"><input type="checkbox" id="research-toggle"> Research public web</label>
        <button class="send" id="send-button" type="submit">Send</button>
      </form>
      <p class="hint">Enter to send · Shift+Enter for a new line</p>
    </div>
    </div>
    <aside class="run-inspector" id="run-inspector" aria-labelledby="run-title" hidden>
      <div class="run-heading"><div><h2 id="run-title">Run Inspector</h2><span class="run-state" id="run-state" role="status">Loading</span></div><button type="button" class="run-close" id="run-close" aria-label="Close Run Inspector">×</button></div>
      <div class="run-meta" id="run-meta">Read-only</div>
      <nav class="run-tabs" role="tablist" aria-label="Run details">
        <button type="button" role="tab" id="run-tab-timeline" aria-controls="run-view-timeline" aria-selected="true" data-run-tab="timeline">Timeline</button>
        <button type="button" role="tab" id="run-tab-context" aria-controls="run-view-context" aria-selected="false" tabindex="-1" data-run-tab="context">Context</button>
        <button type="button" role="tab" id="run-tab-model" aria-controls="run-view-model" aria-selected="false" tabindex="-1" data-run-tab="model">Model call</button>
      </nav>
      <div class="run-scroll" id="run-scroll">
        <div class="run-feedback" id="run-feedback" hidden><p id="run-feedback-text"></p><button type="button" class="reset" id="run-retry">Retry</button></div>
        <p class="run-caption" id="run-truncation" hidden></p>
        <section role="tabpanel" id="run-view-timeline" aria-labelledby="run-tab-timeline" tabindex="0"><p class="run-caption">Execution events, not the model’s private reasoning.</p><div id="run-events"></div><p class="run-caption" id="run-tools"></p></section>
        <section role="tabpanel" id="run-view-context" aria-labelledby="run-tab-context" tabindex="0" hidden><p class="run-caption">Server-supplied context for this turn. Dates use the server timezone.</p><dl id="run-context-values"></dl><h3>Baseline instructions</h3><div class="run-block" id="run-instructions"></div><h3>Messages included in the model context</h3><div id="run-messages"></div></section>
        <section role="tabpanel" id="run-view-model" aria-labelledby="run-tab-model" tabindex="0" hidden><p class="run-caption">Captured provider payloads exclude authorization headers. Private reasoning is not recorded.</p><dl id="run-model-values"></dl><div class="run-block" id="run-error" hidden></div><details id="run-request"><summary>Sanitized request</summary><pre></pre></details><details id="run-response"><summary>Sanitized response</summary><pre></pre></details></section>
      </div>
      <footer class="run-footer">Private to your Agent account. Traces contain chat text; retained for up to 7 days, latest 50 runs. Older chats may have no trace.</footer>
    </aside>
    </div>
    </div>
  </main>
  <dialog class="connection-dialog" id="disconnect-dialog" aria-labelledby="disconnect-title" aria-describedby="disconnect-description">
    <form id="connection-confirm-form">
    <h2 id="disconnect-title">Disconnect ChatGPT?</h2>
    <p id="disconnect-account"></p>
    <p id="disconnect-description">Your saved conversations and subscription are kept.</p>
    <label for="connection-password">Agent password</label>
    <input id="connection-password" name="password" type="password" autocomplete="current-password" maxlength="72" required aria-describedby="connection-confirm-error">
    <p id="connection-confirm-error" class="menu-error" role="alert" hidden></p>
    <div class="dialog-actions"><button class="reset" id="cancel-disconnect" type="button" autofocus>Cancel</button><button class="reset danger" id="confirm-disconnect" type="submit">Disconnect account</button></div>
    </form>
  </dialog>
  <dialog class="connection-dialog" id="usage-dialog" aria-labelledby="usage-dialog-title" aria-describedby="usage-dialog-description">
    <form id="usage-confirm-form"><h2 id="usage-dialog-title">Disconnect usage account?</h2><p id="usage-dialog-account"></p><p id="usage-dialog-description">Remove this usage connection from Agent. Your conversation connection and saved chats are kept. Remote revocation is not guaranteed.</p><label for="usage-password">Agent password</label><input id="usage-password" type="password" autocomplete="current-password" maxlength="72" required><p id="usage-confirm-error" class="menu-error" role="alert" hidden></p><div class="dialog-actions"><button class="reset" id="usage-dialog-cancel" type="button" autofocus>Cancel</button><button class="reset danger" id="usage-confirm" type="submit">Disconnect usage account</button></div></form>
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
      const localConnectionAddress = document.getElementById('local-connection-address');
      const localConnectionURL = document.getElementById('local-connection-url');
      const localConnectionConfigError = document.getElementById('local-connection-config-error');
      const copyLocalConnectionURL = document.getElementById('copy-local-connection-url');
      const providerError = document.getElementById('provider-error');
      const providerFeedback = document.getElementById('provider-feedback');
      const disconnectDialog = document.getElementById('disconnect-dialog');
      let connectionBusy = false;
      let connectionRefresh = null;
      let connectionStateLabel = 'Checking…';
      let activeProfileID = '';
      let selectedProfile = null;
      let connectionChange = null;
      let connectionSubmitting = false;
      const connectionPassword = document.getElementById('connection-password');
      const connectionConfirmForm = document.getElementById('connection-confirm-form');
      const connectionConfirmError = document.getElementById('connection-confirm-error');
      let profileCount = 0;
      let connectionAvailable = false;
      let pendingProfiles = [];
      const localHost = ['localhost', '127.0.0.1', '::1', '[::1]'].includes(window.location.hostname);
      const conversation = [];
      let busy = false;
      let researchFlow = false;
      let researchModelWasDisabled = false;
      let loadingConversation = false;
      let conversationVersion = 0;
      let savedConversations = [];
      let historyError = '';
      let historyRequest = 0;
      let conversationId = '';
      let omittedConversationMessages = 0;
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

      const runPanel = document.getElementById('run-inspector');
      const runOverlay = window.matchMedia('(max-width: 1000px)');
      const pendingRuns = new Set();
      let inspectedRun = '', runEpoch = 0, runTimer = null, runController = null;
      let runOrigin = null, runTab = 'timeline', runSnapshot = null, runPolls = 0;
      function runElement(id) { return document.getElementById('run-' + id); }
      function runNode(tag, text, className) {
        const node = document.createElement(tag);
        if (text !== undefined) node.textContent = text;
        if (className) node.className = className;
        return node;
      }
      function runDuration(milliseconds) {
        return typeof milliseconds === 'number' && Number.isFinite(milliseconds) && milliseconds >= 0
          ? (milliseconds / 1000).toFixed(1) + ' s' : 'Not available';
      }
      function runValues(target, values) {
        target.replaceChildren();
        for (const [label, value] of values) {
          const row = document.createElement('div');
          row.append(runNode('dt', label), runNode('dd', value));
          target.append(row);
        }
      }
      function runSetTab(tab, focus = false) {
        runTab = tab;
        for (const name of ['timeline', 'context', 'model']) {
          const active = name === tab, button = runElement('tab-' + name);
          button.setAttribute('aria-selected', String(active));
          button.tabIndex = active ? 0 : -1;
          runElement('view-' + name).hidden = !active;
        }
        runElement('scroll').scrollTop = 0;
        if (focus) runElement('tab-' + tab).focus();
      }
      function updateSidebarInert() {
        sidebar.inert = (!runPanel.hidden && runOverlay.matches) || (mobile.matches && !chatApp.classList.contains('sidebar-open'));
      }
      function runOverlayState() {
        const overlay = !runPanel.hidden && runOverlay.matches;
        runPanel.setAttribute('role', overlay ? 'dialog' : 'complementary');
        if (overlay) runPanel.setAttribute('aria-modal', 'true');
        else runPanel.removeAttribute('aria-modal');
        document.getElementById('chat-thread').inert = overlay;
        document.querySelector('.workspace-header').inert = overlay;
        updateSidebarInert();
        if (overlay && !runPanel.contains(document.activeElement)) runElement('close').focus({preventScroll:true});
      }
      function stopRunPolling() {
        ++runEpoch;
        clearTimeout(runTimer); runTimer = null;
        if (runController) runController.abort();
        runController = null;
      }
      function closeRunInspector(restoreFocus = true) {
        stopRunPolling();
        inspectedRun = ''; runSnapshot = null; runPanel.hidden = true;
        for (const button of messageList.querySelectorAll('.inspect-run')) button.setAttribute('aria-expanded', 'false');
        runOverlayState();
        runElement('events').replaceChildren(); runElement('messages').replaceChildren();
        for (const name of ['context-values','model-values','instructions']) runElement(name).replaceChildren();
        runElement('request').querySelector('pre').textContent = '';
        runElement('response').querySelector('pre').textContent = '';
        runElement('error').textContent = '';
        if (restoreFocus && runOrigin?.isConnected) runOrigin.focus({preventScroll:true});
        runOrigin = null;
      }
      function runFeedback(message, retry = true) {
        runElement('feedback').hidden = !message;
        runElement('feedback-text').textContent = message;
        runElement('retry').hidden = !retry;
      }
      function renderRun(run) {
        const focusedEvent = document.activeElement.closest('#run-events .run-event');
        const focusedEventKey = focusedEvent?.dataset.eventKey;
        runSnapshot = run;
        const labels = {running:'Running',completed:'Completed',failed:'Failed',interrupted:'Interrupted',cancelled:'Cancelled'};
        const state = labels[run.state] ? run.state : '';
        runElement('state').textContent = labels[state] || 'Unknown';
        runElement('state').className = 'run-state ' + state;
        runElement('meta').textContent = [run.id, run.model || 'Model not selected', runDuration(run.duration_ms), 'Read-only'].join(' · ');
        const events = runElement('events'); events.replaceChildren();
        for (const event of Array.isArray(run.events) ? run.events : []) {
          const titles = {context:'Context prepared',consent:'Research permission approved',tool:'Public-web tool call',provider:'Model request started',completed:'Response received',failed:'Request failed',interrupted:'Run interrupted',cancelled:'Research cancelled'};
          const title = Object.hasOwn(titles, event.name) ? titles[event.name] : event.name;
          const kind = ['cancelled','rejected','interrupted'].includes(event.state) ? 'failed' : ['completed','running','failed'].includes(event.state) ? event.state : '';
          const row = runNode('div', undefined, 'run-event ' + kind);
          row.dataset.eventKey = String(event.name) + '|' + String(event.at);
          const mark = runNode('span', kind === 'failed' ? '!' : kind === 'running' ? '…' : '✓', 'run-event-mark');
          const content = document.createElement('div');
          content.append(runNode('div', title, 'run-event-title'), runNode('div', event.detail, 'run-event-detail'));
          const timestamp = new Date(event.at), start = new Date(run.started_at);
          const elapsed = timestamp - start;
          if (Number.isFinite(elapsed) && elapsed >= 0) content.append(runNode('div', runDuration(elapsed) + ' from start', 'run-event-time'));
          const action = runNode('button', /context/i.test(event.name) ? 'View context →' : 'View model call →');
          action.type = 'button';
          action.addEventListener('click', () => runSetTab(/context/i.test(event.name) ? 'context' : 'model', true));
          content.append(action); row.append(mark, content); events.append(row);
        }
        const context = run.context || {};
        const toolCount = Array.isArray(context.tools) ? context.tools.length : 0;
        const toolNames = toolCount ? context.tools.map(tool => typeof tool === 'string' ? tool : tool?.function?.name || tool?.name || 'Unnamed tool') : [];
        runElement('tools').textContent = toolCount === 0 ? '0 tools available · No web search performed' : toolCount + ' tools available';
        runValues(runElement('context-values'), [
          ['Current date', context.current_date || 'Not recorded'],
          ['Timezone', [context.timezone, context.utc_offset].filter(Boolean).join(' · ') || 'Not recorded'],
          ['Context source', 'Server clock'], ['Instructions', context.instructions_version || 'Not recorded'],
          ['Tools', toolCount === 0 ? 'None available' : toolNames.join(', ')], ['Skills / memory', 'None loaded'],
          ['Excluded before inference', typeof context.omitted_messages === 'number' ? String(context.omitted_messages) : 'Not recorded'],
          ['Excluded from capture', typeof context.capture_omitted_messages === 'number' ? String(context.capture_omitted_messages) : 'Not recorded'],
        ]);
        runElement('instructions').textContent = context.instructions || 'Not recorded';
        const messages = runElement('messages'); messages.replaceChildren();
        for (const message of Array.isArray(context.messages) ? context.messages : []) {
          messages.append(runNode('h3', String(message.role || 'message').toUpperCase()), runNode('div', message.content, 'run-block'));
        }
        const usage = run.usage;
        runValues(runElement('model-values'), [
          ['Provider', run.provider || 'Not selected'], ['Model', run.model || 'Not selected'],
          ['Started', run.started_at || 'Not recorded'], ['Finished', run.finished_at || (state === 'running' ? 'In progress' : 'Not recorded')],
          ['Duration', runDuration(run.duration_ms)], ['HTTP status', run.http_status ? String(run.http_status) : 'Not received'],
          ['Retries', typeof run.retries === 'number' ? String(run.retries) : 'Not recorded'],
          ['Token usage', usage ? [typeof usage.input_tokens === 'number' ? usage.input_tokens + ' input' : '', typeof usage.output_tokens === 'number' ? usage.output_tokens + ' output' : '', typeof usage.total_tokens === 'number' ? usage.total_tokens + ' total' : ''].filter(Boolean).join(' · ') || 'Not reported' : 'Not reported'],
          ['Cost', 'Not available'],
        ]);
        runElement('error').hidden = !run.error;
        runElement('error').textContent = run.error || '';
        const modelCalls = Array.isArray(run.model_calls) ? run.model_calls : [];
        const toolCalls = Array.isArray(run.tool_calls) ? run.tool_calls : [];
        const truncated = run.request_truncated || run.response_truncated || context.truncated || run.capture_omitted_fields > 0 || modelCalls.some(call => call.request_truncated || call.response_truncated) || toolCalls.some(call => call.truncated);
        runElement('truncation').hidden = !truncated;
        runElement('truncation').textContent = 'Capture size limit reached. Some trace content was omitted; this is not a complete payload.';
        const researchPayloads = run.research ? {
          request: {permission:run.research,model_calls:modelCalls.map(call => ({index:call.index,provider:call.provider,model:call.model,state:call.state,http_status:call.http_status,request:call.request,request_truncated:call.request_truncated})),tool_calls:toolCalls.map(call => ({id:call.id,name:call.name,state:call.state,consent_basis:call.consent_basis,arguments:call.arguments,truncated:call.truncated}))},
          response: {response:run.response,model_calls:modelCalls.map(call => ({index:call.index,state:call.state,response:call.response,response_truncated:call.response_truncated,usage:call.usage})),tool_calls:toolCalls.map(call => ({id:call.id,name:call.name,state:call.state,result:call.result,truncated:call.truncated}))},
        } : null;
        for (const field of ['request','response']) {
          const details = runElement(field), data = researchPayloads ? researchPayloads[field] : run[field];
          details.querySelector('pre').textContent = data === undefined || data === null
            ? (state === 'running' ? 'Not captured yet.' : 'Not captured.')
            : typeof data === 'string' ? data : JSON.stringify(data, null, 2);
        }
        if (focusedEvent) {
          const replacement = Array.from(events.querySelectorAll('.run-event')).find(row => row.dataset.eventKey === focusedEventKey)?.querySelector('button');
          (replacement || runElement('tab-' + runTab)).focus({preventScroll:true});
        }
      }
      async function readRun() {
        if (!inspectedRun || runPanel.hidden || document.hidden || runController) return;
        clearTimeout(runTimer);
        const id = inspectedRun, epoch = runEpoch, controller = new AbortController();
        runController = controller;
        const timeout = setTimeout(() => controller.abort(), 10000);
        let poll = false;
        try {
          const response = await fetch('/api/runs/' + encodeURIComponent(id), {cache:'no-store',signal:controller.signal});
          if (epoch !== runEpoch) return;
          if (response.status === 404) {
            runFeedback(pendingRuns.has(id) ? 'Waiting for the server to record this run…' : 'This trace is no longer available, or was not recorded. Older chats and expired traces cannot be inspected.', !pendingRuns.has(id));
            runElement('state').textContent = 'Unavailable';
            runElement('state').className = 'run-state';
            poll = pendingRuns.has(id);
          } else {
            if (!response.ok) throw new Error('read');
            const data = await response.json();
            if (epoch !== runEpoch) return;
            if (!data.run || data.run.id !== id) throw new Error('invalid');
            runFeedback(''); renderRun(data.run);
            poll = data.run.state === 'running';
          }
        } catch (_) {
          if (epoch === runEpoch) runFeedback('Run details could not be loaded. ' + (runSnapshot ? 'Showing the last received snapshot; it may be out of date.' : 'Your conversation is unchanged.'));
        } finally {
          clearTimeout(timeout);
          if (epoch === runEpoch) {
            runController = null;
            if (poll && ++runPolls < 180) runTimer = setTimeout(() => { void readRun(); }, 1000);
            else if (poll) runFeedback('Live monitoring paused after three minutes. Refresh to check this run again.');
          }
        }
      }
      function openRunInspector(id, origin) {
        closeRunInspector(false);
        inspectedRun = id; runOrigin = origin; runPolls = 0;
        runPanel.hidden = false;
        origin.setAttribute('aria-expanded', 'true');
        runElement('state').textContent = 'Loading'; runElement('state').className = 'run-state';
        runElement('meta').textContent = id + ' · Read-only';
        runElement('tools').textContent = '';
        runElement('error').hidden = true; runElement('truncation').hidden = true;
        runElement('request').open = false; runElement('response').open = false;
        runFeedback(''); runSetTab('timeline'); runOverlayState();
        runElement('close').focus({preventScroll:true});
        void readRun();
      }
      function attachRun(bubble, id) {
        if (!id || typeof id !== 'string') return;
        const button = runNode('button', 'Inspect run', 'inspect-run');
        button.type = 'button'; button.dataset.runId = id;
        button.setAttribute('aria-controls', 'run-inspector');
        button.setAttribute('aria-expanded', String(id === inspectedRun));
        button.addEventListener('click', () => id === inspectedRun ? closeRunInspector() : openRunInspector(id, button));
        bubble.append(button);
        if (id === inspectedRun) runOrigin = button;
      }
      function newRunID() {
        if (typeof crypto.randomUUID === 'function') return crypto.randomUUID();
        const bytes = crypto.getRandomValues(new Uint8Array(16));
        bytes[6] = (bytes[6] & 15) | 64; bytes[8] = (bytes[8] & 63) | 128;
        const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
        return [hex.slice(0,8),hex.slice(8,12),hex.slice(12,16),hex.slice(16,20),hex.slice(20)].join('-');
      }
      runElement('close').addEventListener('click', () => closeRunInspector());
      runElement('retry').addEventListener('click', () => { runPolls = 0; void readRun(); });
      document.querySelectorAll('[data-run-tab]').forEach(button => button.addEventListener('click', () => runSetTab(button.dataset.runTab)));
      runElement('inspector').addEventListener('keydown', event => {
        if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); closeRunInspector(); }
        if (event.target.matches('[data-run-tab]') && ['ArrowLeft','ArrowRight','Home','End'].includes(event.key)) {
          event.preventDefault();
          const names = ['timeline','context','model']; let index = names.indexOf(runTab);
          if (event.key === 'Home') index = 0;
          if (event.key === 'End') index = 2;
          if (event.key === 'ArrowLeft') index = (index + 2) % 3;
          if (event.key === 'ArrowRight') index = (index + 1) % 3;
          runSetTab(names[index], true);
        }
        if (event.key === 'Tab' && runOverlay.matches) {
          const controls = Array.from(runPanel.querySelectorAll('button:not([disabled]), summary, [tabindex="0"]')).filter(node => node.getClientRects().length && node.tabIndex >= 0);
          const first = controls[0], last = controls[controls.length - 1];
          if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
          else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
        }
      });
      document.addEventListener('keydown', event => {
        if (event.key === 'Escape' && !runPanel.hidden && !document.querySelector('dialog[open]')) closeRunInspector();
      });
      runOverlay.addEventListener('change', runOverlayState);
      document.addEventListener('visibilitychange', () => {
        if (document.hidden) stopRunPolling();
        else if (inspectedRun && !runPanel.hidden) void readRun();
      });

      // Usage authorization is independent of the conversation connection.
      const usageElement = id => document.getElementById('usage-' + id);
      const usagePanel = usageElement('panel'), usageDialog = usageElement('dialog');
      let usageProfile = '', usageData = null, usageEpoch = 0, usageTimer = null;
      let usageBusy = false, usageTarget = null, usageSubmitting = false;
      // BEGIN update reload safety
      // Only booleans and a revision leave this closure; drafts/passwords stay in memory.
      let updateDraftRevision = 0;
      document.addEventListener('input', () => { ++updateDraftRevision; });
      window.agentUpdateSafety = () => ({
        pending: busy || loadingConversation || passwordBusy || authBusy || connectionBusy || connectionSubmitting || usageBusy || usageSubmitting || searchBusy,
        unsaved: Boolean(searchKey.value || input.value || Array.from(document.querySelectorAll('input[type="password"], input[autocomplete="current-password"], input[autocomplete="new-password"]')).some(field => field.value) || document.querySelector('dialog[open]')),
        revision: updateDraftRevision,
      });
      // END update reload safety
      function stopUsage() {
        ++usageEpoch;
        clearTimeout(usageTimer);
        usageTimer = null;
        usageBusy = false;
        usageProfile = '';
        usageData = null;
        usagePanel.hidden = true;
      }
      function usageVisible() { return !settingsPanel.hidden && !chatApp.hidden && !document.hidden && Boolean(activeProfileID); }
      function syncUsage() {
        if (!usageVisible()) { stopUsage(); return; }
        if (usageProfile === activeProfileID) return;
        usageProfile = activeProfileID;
        usageData = null;
        usagePanel.hidden = false;
        void usageRequest('status');
      }
      function usagePoll() {
        clearTimeout(usageTimer);
        if (usageVisible() && ['pending', 'checking'].includes(usageData?.state)) {
          usageTimer = setTimeout(() => { void usageRequest('status'); }, 2000);
        }
      }
      function usageDate(value) {
        if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return '';
        const date = new Date(value * 1000);
        return Number.isFinite(date.getTime()) ? date.toLocaleString() : '';
      }
      function renderUsage() {
        if (!usageData) {
          usageElement('badge').textContent = 'Checking…';
          for (const id of ['connect', 'disconnect', 'device', 'identity']) usageElement(id).hidden = true;
          usageElement('windows').replaceChildren();
          usageElement('message').textContent = 'Checking usage availability…';
          usageElement('updated').textContent = '';
        }
        const data = usageData || {}, state = data.state;
        const mismatch = data.identity_match === 'different';
        const stale = Boolean(data.snapshot?.stale);
        const badge = usageElement('badge');
        badge.textContent = usageBusy ? (usageData ? 'Refreshing…' : 'Checking…') : mismatch ? 'Review account' : stale ? 'Last known' : ({connected:'Connected',disconnected:'Optional',pending:'Awaiting approval',checking:'Checking…',expired:'Sign-in expired',unavailable:'Unavailable'}[state] || 'Unavailable');
        badge.className = 'connection-badge' + (state === 'connected' && !mismatch && !stale ? '' : ['disconnected','unavailable'].includes(state) ? ' off' : ' warning');
        usageElement('refresh').disabled = usageBusy || !usageData || !['connected','unavailable'].includes(state) || mismatch || usageDialog.open;
        usageElement('refresh').setAttribute('aria-busy', String(usageBusy));
        usageElement('connect').hidden = !usageData || ['pending','checking'].includes(state) || (state === 'connected' && !mismatch);
        usageElement('connect').disabled = usageBusy || data.available !== true || usageDialog.open;
        usageElement('connect').textContent = mismatch ? 'Use another usage account' : state === 'expired' ? 'Reconnect usage account' : 'Connect usage account';
        usageElement('disconnect').hidden = !data.connection_id || ['pending','checking'].includes(state);
        usageElement('disconnect').disabled = usageBusy || usageDialog.open;
        usageElement('message').textContent = mismatch ? 'This usage account is different. Allowance is hidden. Reconnect with the intended account and workspace, or disconnect usage.' : data.available === false ? 'Usage service unavailable. The Codex dependency is unavailable; your conversation connection is unchanged.' : stale ? 'Showing last-known allowance; these values may no longer be current.' : state === 'expired' ? 'Sign in again to see allowance. Your conversation connection is unchanged.' : state === 'connected' ? 'Codex-reported allowance. Account and workspace matching are not verified by email alone.' : state === 'checking' ? 'Checking the signed-in usage account…' : state === 'pending' ? 'Waiting for OpenAI approval. Your conversation connection is unchanged.' : 'See remaining allowance and reset times with a separate Codex sign-in. Your conversation connection won’t change.';
        usageElement('identity').hidden = !data.identity;
        usageElement('identity').textContent = data.identity ? 'Usage account: ' + (data.identity.email || 'Email unavailable') + (data.identity.plan_type ? ' · ' + data.identity.plan_type : '') + ' · separate Codex sign-in' : '';
        usageElement('error').textContent = data.error || '';
        usageElement('error').hidden = !data.error;
        const login = data.login;
        usageElement('device').hidden = !login || !['pending','checking'].includes(state);
        usageElement('code').textContent = typeof login?.user_code === 'string' ? login.user_code : '';
        usageElement('expiry').textContent = usageDate(login?.expires_at) ? 'Code expires ' + usageDate(login.expires_at) + ' · local time' : 'Code expiry unavailable.';
        const link = usageElement('verification');
        link.hidden = true; link.removeAttribute('href');
        try {
          const url = new URL(login?.verification_url);
          if (url.protocol === 'https:' && url.hostname === 'auth.openai.com' && !url.port && !url.username && !url.password && url.pathname === '/codex/device' && !url.search && !url.hash) { link.href = url.href; link.hidden = false; }
        } catch (_) {}
        usageElement('cancel').disabled = usageBusy;
        const windows = usageElement('windows'); windows.replaceChildren();
        windows.classList.toggle('usage-stale', stale);
        if ((state === 'connected' || (state === 'unavailable' && stale)) && !mismatch) {
          for (const window of Array.isArray(data.snapshot?.windows) ? data.snapshot.windows : []) {
            const row = document.createElement('div'); row.className = 'quota-window';
            const label = document.createElement('div'); label.className = 'quota-label';
            const name = document.createElement('span'); name.textContent = window.label || 'Codex quota window';
            const amount = document.createElement('strong');
            const valid = typeof window.used_percent === 'number' && Number.isFinite(window.used_percent) && window.used_percent >= 0 && window.used_percent <= 100;
            const remaining = valid ? Math.round((100 - window.used_percent) * 10) / 10 : null;
            amount.textContent = valid ? remaining + '% left' : 'Unavailable';
            label.append(name, amount); row.append(label);
            if (valid) {
              const track = document.createElement('div'); track.className = 'quota-track'; track.setAttribute('role', 'meter'); track.setAttribute('aria-label', name.textContent + ' remaining' + (stale ? ' (last known)' : '')); track.setAttribute('aria-valuemin', '0'); track.setAttribute('aria-valuemax', '100'); track.setAttribute('aria-valuenow', String(remaining));
              const fill = document.createElement('div'); fill.className = 'quota-fill'; fill.style.width = remaining + '%'; track.append(fill); row.append(track);
            }
            const reset = document.createElement('div'); reset.className = 'quota-reset';
            reset.textContent = (usageDate(window.reset_at) ? 'Resets ' + usageDate(window.reset_at) + ' · local time' : 'Reset time unavailable') + (typeof window.window_minutes === 'number' && Number.isFinite(window.window_minutes) && window.window_minutes > 0 ? ' · ' + window.window_minutes + '-minute window' : '');
            row.append(reset); windows.append(row);
          }
          if (!windows.childElementCount) { const unavailable = document.createElement('p'); unavailable.textContent = 'Allowance windows are unavailable. Refresh to try again.'; windows.append(unavailable); }
        }
        usageElement('updated').textContent = !mismatch && data.snapshot ? (usageDate(data.snapshot.fetched_at) ? 'Updated ' + usageDate(data.snapshot.fetched_at) + ' · local time' : 'Update time unavailable') + (stale ? ' · stale' : '') : '';
      }
      async function usageRequest(action, captured) {
        if (!usageVisible() || usageBusy || !usageProfile) return false;
        const priorState = usageData?.state;
        let firstFetch = false;
        const profile = usageProfile, epoch = ++usageEpoch;
        const target = captured || {profile_id:profile,connection_id:usageData?.connection_id || ''};
        clearTimeout(usageTimer);
        usageBusy = true;
        usageElement('feedback').textContent = '';
        renderUsage();
        try {
          const payload = {...target};
          if (action === 'connect') { payload.expected_connection_id = payload.connection_id; delete payload.connection_id; }
          const response = await fetch('/api/usage/' + action + (action === 'status' ? '?profile_id=' + encodeURIComponent(profile) : ''), action === 'status' ? {cache:'no-store'} : {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)});
          if ('password' in payload) payload.password = '';
          const result = await response.json();
          if (epoch !== usageEpoch || profile !== activeProfileID || profile !== usageProfile || !usageVisible()) return false;
          if (!response.ok) throw new Error(result.error || 'Usage request failed. Please try again.');
          if (result.profile_id !== profile) throw new Error('Usage account changed. Refresh Settings to retry.');
          firstFetch = action === 'status' && ['pending','checking'].includes(priorState) && result.state === 'connected' && !result.snapshot && result.identity_match !== 'different';
          usageData = result;
          return true;
        } catch (error) {
          if (epoch !== usageEpoch || profile !== usageProfile || !usageVisible()) return false;
          if (usageData?.snapshot) usageData = {...usageData,snapshot:{...usageData.snapshot,stale:true},error:error.message};
          else usageData = {...(usageData || {profile_id:profile,available:false,state:'unavailable',connection_id:'',identity_match:'unverified'}),error:error.message};
          if (action === 'disconnect' && usageDialog.open) { usageElement('confirm-error').textContent = error.message; usageElement('confirm-error').hidden = false; }
          // A failed mutation may already have saved a new connection record.
          // Reload its precondition before allowing retry, never retarget a dialog.
          if (action !== 'status') {
            try {
              const recovery = await fetch('/api/usage/status?profile_id=' + encodeURIComponent(profile), {cache:'no-store'});
              const current = await recovery.json();
              if (epoch === usageEpoch && profile === usageProfile && profile === activeProfileID && usageVisible() && recovery.ok && current.profile_id === profile) {
                usageData = {...current,error:error.message};
                if (action === 'refresh' && current.snapshot) usageData.snapshot = {...current.snapshot,stale:true};
              }
            } catch (_) {} // Keep the original actionable failure and last-known data.
          }
          return false;
        } finally {
          if (captured && 'password' in captured) captured.password = '';
          if (epoch === usageEpoch) { usageBusy = false; renderUsage(); if (firstFetch) void usageRequest('refresh'); else usagePoll(); }
        }
      }
      usageElement('connect').addEventListener('click', () => { void usageRequest('connect'); });
      usageElement('refresh').addEventListener('click', () => { void usageRequest(usageData?.state === 'unavailable' ? 'status' : 'refresh'); });
      usageElement('cancel').addEventListener('click', () => {
        if (usageData?.login) void usageRequest('cancel', {profile_id:usageProfile,connection_id:usageData.connection_id,login_id:usageData.login.login_id});
      });
      usageElement('copy').addEventListener('click', async () => {
        const code = usageElement('code').textContent;
        try { await navigator.clipboard.writeText(code); usageElement('feedback').textContent = 'Code copied.'; }
        catch (_) { usageElement('feedback').textContent = 'Copy unavailable. Select and copy the code manually.'; }
      });
      usageElement('disconnect').addEventListener('click', () => {
        if (usageBusy || !usageData?.connection_id) return;
        usageTarget = Object.freeze({profile_id:usageProfile,connection_id:usageData.connection_id});
        usageElement('password').value = '';
        usageElement('confirm-error').hidden = true;
        usageElement('dialog-account').textContent = (usageData.identity?.email || selectedProfile?.email || 'Selected account') + ' · usage connection only';
        usageDialog.showModal(); renderUsage(); usageElement('dialog-cancel').focus();
      });
      usageElement('dialog-cancel').addEventListener('click', () => { if (!usageSubmitting) usageDialog.close(); });
      usageDialog.addEventListener('cancel', event => { if (usageSubmitting) event.preventDefault(); });
      usageDialog.addEventListener('close', () => {
        if (usageDialog.open) return;
        usageTarget = null; usageElement('password').value = ''; usageElement('confirm-error').textContent = ''; usageElement('confirm-error').hidden = true;
        renderUsage();
        if (usageVisible()) (usageElement('disconnect').hidden ? usageElement('connect') : usageElement('disconnect')).focus();
      });
      usageElement('confirm-form').addEventListener('submit', async event => {
        event.preventDefault();
        if (!usageTarget || usageSubmitting || !usageDialog.open) return;
        const target = {...usageTarget,password:usageElement('password').value};
        usageElement('password').value = '';
        usageSubmitting = true;
        usageElement('confirm-error').hidden = true;
        const controls = Array.from(usageElement('confirm-form').elements);
        controls.forEach(control => { control.disabled = true; });
        usageElement('confirm-form').setAttribute('aria-busy','true');
        try { if (await usageRequest('disconnect', target)) usageDialog.close(); }
        finally { target.password = ''; usageElement('password').value = ''; usageSubmitting = false; controls.forEach(control => { control.disabled = false; }); usageElement('confirm-form').setAttribute('aria-busy','false'); if (usageDialog.open) usageElement('password').focus(); }
      });
      document.addEventListener('visibilitychange', () => { if (document.hidden) stopUsage(); else syncUsage(); });

      // Search credentials never leave this input except in the explicit verify POST.
      const searchEl = id => document.getElementById(id);
      const searchKey = searchEl('research-api-key'), searchDialog = searchEl('research-key-remove-dialog');
      let searchData = null, searchBusy = false, searchEpoch = 0, searchEditing = false;
      let searchExpectedRevision = '', searchRemoveRevision = '', searchUnknown = false;
      function clearSearchDraft() {
        searchKey.value = ''; searchKey.type = 'password';
        searchEl('research-key-show').textContent = 'Show';
        searchEl('research-key-show').setAttribute('aria-pressed', 'false');
        searchEditing = false; ++updateDraftRevision;
      }
      function renderSearchSettings() {
        const known = Boolean(searchData) && !searchUnknown;
        const badge = searchEl('research-settings-badge');
        badge.textContent = searchBusy ? 'Checking…' : !known ? 'Status unknown' : !searchData.configured ? 'Not configured' : ({connected:'Last test passed',invalid:'Last test: invalid key',unavailable:'Last test unavailable'}[searchData.status] || 'Not verified');
        badge.className = 'connection-badge ' + (known && searchData.status === 'connected' ? '' : 'off');
        searchEl('research-key-saved').hidden = !known || !searchData.configured;
        searchEl('research-key-form').hidden = !known || (searchData.configured && !searchEditing);
        searchEl('research-key-suffix').textContent = known && searchData.configured ? '•••• ' + searchData.key_suffix : '';
        searchEl('research-key-checked').textContent = known && searchData.configured ? 'Last verification: ' + (searchData.last_checked_at || searchData.verified_at || 'not recorded') + '. This is not a live availability guarantee.' : '';
        searchEl('research-settings-retry').hidden = known || searchBusy;
        searchEl('research-settings-card').setAttribute('aria-busy', String(searchBusy));
        for (const node of searchEl('research-settings-card').querySelectorAll('button, input')) node.disabled = searchBusy || (!known && node.id !== 'research-settings-retry');
        searchEl('research-key-remove-confirm').disabled = searchBusy;
        searchEl('research-key-keep').disabled = searchBusy;
      }
      async function searchFetch(action, expectedRevision) {
        const controller = new AbortController();
        const timeout = setTimeout(() => controller.abort(), 12000);
        try {
          const response = await fetch('/api/research/settings' + (action ? '/' + action : ''), {
            method: action ? 'POST' : 'GET', cache:'no-store', signal:controller.signal,
            ...(action ? {headers:{'Content-Type':'application/json'}, body:JSON.stringify(action === 'verify' ? {api_key:searchKey.value,expected_revision:expectedRevision} : {expected_revision:expectedRevision})} : {})
          });
          const result = await response.json();
          return {response,result};
        } finally { clearTimeout(timeout); }
      }
      function acceptSearchSettings(data) {
        if (!data || typeof data.configured !== 'boolean' || typeof data.revision !== 'string' || typeof data.key_suffix !== 'string') throw new Error('Invalid metadata');
        searchData = data; searchUnknown = false;
      }
      async function readSearchSettings() {
        if (searchBusy || settingsPanel.hidden) return;
        const epoch = ++searchEpoch;
        searchBusy = true; searchUnknown = true; searchEl('research-key-error').textContent = ''; renderSearchSettings();
        try {
          const {response,result} = await searchFetch('');
          if (epoch !== searchEpoch) return;
          if (!response.ok) throw new Error('Read failed');
          acceptSearchSettings(result.settings);
          if (!searchEditing) searchExpectedRevision = searchData.revision;
        } catch (_) {
          if (epoch === searchEpoch) { searchUnknown = true; searchEl('research-key-error').textContent = 'Could not load your search settings. Retry to review the current saved key.'; }
        } finally {
          searchBusy = false;
          if (epoch === searchEpoch) renderSearchSettings();
          else if (!settingsPanel.hidden) void readSearchSettings();
        }
      }
      async function mutateSearchSettings(action, expectedRevision) {
        if (searchBusy || searchUnknown || !searchData) return;
        if (action === 'verify' && !searchKey.value.trim()) { searchEl('research-key-error').textContent = 'Enter a Brave Search API key.'; searchKey.focus(); return; }
        const epoch = searchEpoch;
        searchBusy = true; searchEl('research-key-error').textContent = ''; searchEl('research-key-feedback').textContent = ''; renderSearchSettings();
        let message = '', success = false;
        try {
          const {response,result} = await searchFetch(action, expectedRevision);
          if (response.ok) {
            acceptSearchSettings(result.settings); success = true;
            if (action !== 'check') clearSearchDraft();
          } else if (response.status === 409) {
            clearSearchDraft();
            const current = await searchFetch('');
            if (!current.response.ok) throw new Error('Reconcile failed');
            acceptSearchSettings(current.result.settings);
            message = 'Your saved key changed elsewhere. Review the current key, then choose your action again.';
          } else if (response.status === 422 || result.code === 'invalid_key' || response.status === 429 || response.status === 503) {
            if (!result.settings) throw new Error('Mutation outcome was not acknowledged');
            acceptSearchSettings(result.settings);
            message = result.code === 'invalid_key' || response.status === 422 ? 'Brave rejected this key. Check it and try again. Your previously saved key has not been replaced.' : response.status === 429 ? 'Brave rate limit reached. Wait before trying again. Your saved key has not been replaced.' : 'Brave is unavailable. Try again later. Your saved key has not been replaced.';
          } else throw new Error('Ambiguous mutation');
        } catch (_) {
          // Aborting a fetch does not roll back a committed server mutation. Never replay it.
          clearSearchDraft(); searchUnknown = true;
          try {
            const current = await searchFetch('');
            if (!current.response.ok) throw new Error('Reconcile failed');
            acceptSearchSettings(current.result.settings);
            message = 'The operation was not acknowledged. Current saved settings have been refreshed; review them before trying again.';
          } catch (_) { message = 'Outcome unknown: the server may have saved this change. Retry settings to review it before making another change.'; }
        } finally {
          searchBusy = false;
          if (searchDialog.open) searchDialog.close();
          if (epoch === searchEpoch && !settingsPanel.hidden) {
            if (!searchEditing && searchData) searchExpectedRevision = searchData.revision;
            searchEl('research-key-error').textContent = message;
            searchEl('research-key-feedback').textContent = success ? (action === 'verify' ? 'Brave key verified and saved. Research still needs task permission.' : action === 'remove' ? 'Brave key removed here, not revoked at Brave.' : 'One connection test completed. See the last verification status above.') : '';
            renderSearchSettings();
            if (success && action === 'verify') searchEl('research-key-replace').focus();
            if (success && action === 'remove') searchKey.focus();
          } else if (!settingsPanel.hidden) void readSearchSettings();
        }
      }
      searchEl('research-key-form').addEventListener('submit', event => { event.preventDefault(); void mutateSearchSettings('verify', searchExpectedRevision); });
      searchEl('research-key-show').addEventListener('click', () => {
        const show = searchKey.type === 'password'; searchKey.type = show ? 'text' : 'password';
        searchEl('research-key-show').textContent = show ? 'Hide' : 'Show';
        searchEl('research-key-show').setAttribute('aria-pressed', String(show));
      });
      searchEl('research-key-replace').addEventListener('click', () => {
        if (searchBusy || searchUnknown) return;
        clearSearchDraft(); searchEditing = true; searchExpectedRevision = searchData.revision;
        searchEl('research-key-error').textContent = ''; searchEl('research-key-feedback').textContent = '';
        renderSearchSettings(); searchKey.focus();
      });
      searchEl('research-key-cancel').addEventListener('click', () => {
        if (searchBusy) return;
        clearSearchDraft(); searchEl('research-key-error').textContent = ''; renderSearchSettings();
        if (searchData?.configured) searchEl('research-key-replace').focus(); else searchKey.focus();
      });
      searchEl('research-key-check').addEventListener('click', () => { void mutateSearchSettings('check', searchData?.revision); });
      searchEl('research-settings-retry').addEventListener('click', () => { void readSearchSettings(); });
      searchEl('research-key-remove').addEventListener('click', () => {
        if (searchBusy || searchUnknown) return;
        searchRemoveRevision = searchData.revision; searchDialog.showModal(); searchEl('research-key-keep').focus();
      });
      searchEl('research-key-keep').addEventListener('click', () => { if (!searchBusy) searchDialog.close(); });
      searchDialog.addEventListener('cancel', event => { if (searchBusy) event.preventDefault(); });
      searchDialog.addEventListener('close', () => { if (!settingsPanel.hidden) searchEl('research-key-remove').focus(); });
      searchEl('research-key-remove-confirm').addEventListener('click', () => { void mutateSearchSettings('remove', searchRemoveRevision); });
      function leaveSearchSettings() {
        ++searchEpoch; clearSearchDraft(); searchUnknown = true;
        if (searchDialog.open) searchDialog.close();
        searchEl('research-key-feedback').textContent = ''; renderSearchSettings();
      }
      window.addEventListener('pagehide', clearSearchDraft);

      function showSettings(open, updateURL = true) {
        if (researchFlow) return;
        if (open) closeRunInspector(false);
        if (!open) { stopUsage(); leaveSearchSettings(); }
        if (updateURL && (window.location.hash === '#settings') !== open) {
          const url = new URL(window.location.href);
          url.hash = open ? 'settings' : '';
          window.history.pushState(null, '', url);
        }
        settingsPanel.hidden = !open;
        document.getElementById('settings-title').hidden = !open;
        conversationTitle.hidden = open;
        document.getElementById('conversation-save-status').hidden = open || !notice.hidden;
        messageList.hidden = open;
        document.getElementById('chat-layout').hidden = open;
        document.querySelector('.composer-wrap').hidden = open;
        settingsButton.setAttribute('aria-pressed', String(open));
        closeAccount(false);
        setSidebar(false, false);
        if (!passwordBusy) { passwordForm.reset(); passwordStatus.textContent = ''; }
        if (open) { void readSearchSettings(); document.getElementById('back-to-chat').focus(); if (!connectionBusy) refreshConnectionState(); }
        else input.focus();
      }
      settingsButton.addEventListener('click', function () { showSettings(true); });
      document.getElementById('manage-connections').addEventListener('click', function () { showSettings(true); });
      document.getElementById('back-to-chat').addEventListener('click', function () { showSettings(false); });
      function restorePage() {
        if (!chatApp.hidden) showSettings(window.location.hash === '#settings', false);
      }
      window.addEventListener('popstate', restorePage);
      window.addEventListener('hashchange', restorePage);
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
        updateSidebarInert();
        if (!expanded) closeAccount(false);
        if (expanded) document.getElementById('new-chat').focus({ preventScroll: true });
        else if (restoreFocus && mobile.matches) sidebarToggle.focus();
      }
      sidebarToggle.addEventListener('click', function () { setSidebar(!chatApp.classList.contains('sidebar-open'), true); });
      backdrop.addEventListener('click', function () { setSidebar(false, true); });
      mobile.addEventListener('change', function () { setSidebar(false, false); });
      setSidebar(false, false);
      const sidebarResize = document.getElementById('sidebar-resize');
      function setSidebarWidth(width) {
        const max = Math.min(480, window.innerWidth * .45);
        width = Math.round(Math.max(180, Math.min(max, width)));
        chatApp.style.setProperty('--sidebar-width', width + 'px');
        sidebarResize.setAttribute('aria-valuenow', String(width));
        sidebarResize.setAttribute('aria-valuemax', String(Math.floor(max)));
      }
      function resizeWithPointer(handle, start) {
        handle.addEventListener('pointerdown', function (event) {
          if (event.button !== 0) return;
          const move = start(event);
          if (!move) return;
          event.preventDefault();
          handle.setPointerCapture(event.pointerId);
          function moving(next) { if (next.pointerId === event.pointerId) move(next); }
          function finish(next) {
            if (next.pointerId !== event.pointerId) return;
            window.removeEventListener('pointermove', moving);
            handle.removeEventListener('lostpointercapture', finish);
            window.removeEventListener('pointerup', finish);
            window.removeEventListener('pointercancel', finish);
            if (handle.hasPointerCapture(event.pointerId)) handle.releasePointerCapture(event.pointerId);
          }
          window.addEventListener('pointermove', moving);
          handle.addEventListener('lostpointercapture', finish);
          window.addEventListener('pointerup', finish);
          window.addEventListener('pointercancel', finish);
        });
      }
      resizeWithPointer(sidebarResize, function (event) {
        if (mobile.matches) return;
        const width = sidebar.getBoundingClientRect().width, x = event.clientX;
        return next => { if (!mobile.matches) setSidebarWidth(width + next.clientX - x); };
      });
      const composerResize = document.getElementById('composer-resize');
      function setComposerHeight(height) {
        input.style.height = Math.max(42, Math.min(400, window.innerHeight * .45, height)) + 'px';
      }
      resizeWithPointer(composerResize, function (event) {
        const height = input.getBoundingClientRect().height, y = event.clientY;
        return next => setComposerHeight(height + y - next.clientY);
      });
      composerResize.addEventListener('keydown', function (event) {
        if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return;
        event.preventDefault();
        setComposerHeight(input.getBoundingClientRect().height + (event.key === 'ArrowUp' ? 16 : -16));
      });
      sidebarResize.addEventListener('keydown', function (event) {
        if (mobile.matches) return;
        const width = sidebar.getBoundingClientRect().width;
        const values = { ArrowLeft: width - 16, ArrowRight: width + 16, Home: 180, End: 480 };
        if (!(event.key in values)) return;
        event.preventDefault();
        setSidebarWidth(values[event.key]);
      });
      window.addEventListener('resize', function () {
        if (!chatApp.hidden && !mobile.matches) setSidebarWidth(sidebar.getBoundingClientRect().width);
      });
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
        researchToggle.disabled = locked;
        settingsButton.disabled = researchFlow;
        document.getElementById('logout-button').disabled = researchFlow;
        if (researchFlow) modelSelect.disabled = true;
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
            const title = document.createElement('span');
            title.className = 'session-title';
            title.textContent = item.title || 'Conversation';
            button.title = title.textContent;
            button.appendChild(title);
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
          restorePage();
          connectionStatus.textContent = 'Signed in as ' + user.username + '.';
          await refreshConnectionState();
          await refreshConversations();
        } catch (_) { stopUsage(); authGate.hidden = false; chatApp.hidden = true; }
      }

      document.getElementById('logout-button').addEventListener('click', async function () {
        if (researchFlow) return;
        if (searchBusy) return;
        leaveSearchSettings();
        const button = this;
        const error = document.getElementById('logout-error');
        error.hidden = true; button.disabled = true;
        try {
          const response = await fetch('/api/auth/logout', { method: 'POST' });
          if (!response.ok) throw new Error('Sign out failed. Please try again.');
          stopUsage();
          window.location.reload();
        } catch (_) { error.textContent = 'Sign out failed. Please try again.'; error.hidden = false; }
        finally { button.disabled = false; }
      });

      async function loadConversation(item) {
        if (busy || loadingConversation) return;
        researchToggle.checked = false;
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
          closeRunInspector(false);
          omittedConversationMessages = Number(result.omitted_messages) || 0;
          historyError = '';
          conversationTitle.textContent = (result.conversation && result.conversation.title) || item.title || 'Conversation';
          conversation.length = 0;
          messageList.replaceChildren();
          input.value = '';
          for (const item of result.messages || []) { const bubble = addMessage(item.role, item.content); if (item.role === 'assistant') attachRun(bubble, item.run_id); conversation.push({ role: item.role, content: item.content }); }
          setConversationNotice('This conversation is saved to your account.', true);
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

      // A deliberately small Markdown subset. Build DOM nodes, never interpret
      // model output as HTML or load embedded images/resources.
      function appendMessageInline(parent, value, depth = 0) {
        if (depth > 6) { parent.append(document.createTextNode(value)); return; }
        const tokens = /\x60([^\x60\n]+)\x60|\*\*([^\n]+?)\*\*|\*([^*\n]+)\*|\[([^\]\n]+)\]\(([^\s)]+)\)/g;
        let offset = 0;
        for (const match of value.matchAll(tokens)) {
          parent.append(document.createTextNode(value.slice(offset, match.index)));
          let node;
          if (match[1] !== undefined) {
            node = document.createElement('code');
            node.textContent = match[1];
          } else if (match[2] !== undefined || match[3] !== undefined) {
            node = document.createElement(match[2] !== undefined ? 'strong' : 'em');
            appendMessageInline(node, match[2] ?? match[3], depth + 1);
          } else {
            let url;
            try { url = new URL(match[5]); } catch (_) {}
            if (url && (url.protocol === 'https:' || url.protocol === 'http:')) {
              node = document.createElement('a');
              node.href = url.href;
              node.target = '_blank';
              node.rel = 'noopener noreferrer';
              appendMessageInline(node, match[4], depth + 1);
            } else {
              node = document.createTextNode(match[0]);
            }
          }
          parent.append(node);
          offset = match.index + match[0].length;
        }
        parent.append(document.createTextNode(value.slice(offset)));
      }

      function renderMessageText(parent, content) {
        const lines = content.replace(/\r\n?/g, '\n').split('\n');
        const blockStart = line => /^(?:\x60{3,}|~{3,}|#{1,6}\s|\s*[-+*]\s|\s*\d+[.)]\s|>\s?)/.test(line);
        for (let i = 0; i < lines.length;) {
          const line = lines[i];
          if (!line.trim()) { i++; continue; }
          const fence = line.match(/^(\x60{3,}|~{3,})[^\n]*$/);
          if (fence) {
            const body = [];
            i++;
            while (i < lines.length) {
              const close = lines[i].match(/^(\x60{3,}|~{3,})\s*$/);
              if (close && close[1][0] === fence[1][0] && close[1].length >= fence[1].length) { i++; break; }
              body.push(lines[i++]);
            }
            const pre = document.createElement('pre');
            pre.tabIndex = 0;
            pre.setAttribute('aria-label', 'Code block');
            const code = document.createElement('code');
            code.textContent = body.join('\n');
            pre.append(code);
            parent.append(pre);
            continue;
          }
          const heading = line.match(/^(#{1,6})\s+(.+)$/);
          if (heading) {
            const node = document.createElement('h' + Math.min(4, Math.max(2, heading[1].length)));
            appendMessageInline(node, heading[2]);
            parent.append(node); i++; continue;
          }
          const listItem = line.match(/^\s*([-+*]|\d+[.)])\s+(.+)$/);
          if (listItem) {
            const ordered = /^\d/.test(listItem[1]);
            const list = document.createElement(ordered ? 'ol' : 'ul');
            if (ordered) list.start = Number.parseInt(listItem[1], 10);
            while (i < lines.length) {
              const item = lines[i].match(/^\s*([-+*]|\d+[.)])\s+(.+)$/);
              if (!item || /^\d/.test(item[1]) !== ordered) break;
              const li = document.createElement('li');
              appendMessageInline(li, item[2]);
              list.append(li); i++;
            }
            parent.append(list); continue;
          }
          const quote = /^>\s?/.test(line);
          const body = [];
          do {
            body.push(quote ? lines[i].replace(/^>\s?/, '') : lines[i]);
            i++;
          } while (i < lines.length && lines[i].trim() && (quote ? /^>\s?/.test(lines[i]) : !blockStart(lines[i])));
          const node = document.createElement(quote ? 'blockquote' : 'p');
          appendMessageInline(node, body.join('\n'));
          parent.append(node);
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
        if (role === 'assistant') renderMessageText(text, content);
        else text.textContent = content;
        bubble.append(sender, text);
        messageList.appendChild(bubble);
        messageList.scrollTop = messageList.scrollHeight;
        return bubble;
      }

      function setConversationNotice(message, saved = false) {
        notice.textContent = saved ? '' : message;
        notice.hidden = saved;
        document.getElementById('conversation-save-status').hidden = !saved || !settingsPanel.hidden;
      }

      function showError(message, action) {
        setConversationNotice(message);
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
        for (const button of [connectButton, addAccountButton, disconnectButton, checkConnectionButton, copyLocalConnectionURL]) button.disabled = pending;
        accountSelect.disabled = pending || profileCount === 0 || (profileCount === 1 && Boolean(activeProfileID));
        connectionBadge.textContent = connectionRefresh ? 'Checking…' : connectionStateLabel;
        checkConnectionButton.setAttribute('aria-busy', String(Boolean(connectionRefresh)));
        checkConnectionButton.setAttribute('aria-label', connectionRefresh ? 'Checking ChatGPT connection status' : connectionStateLabel + '. Refresh ChatGPT connection status');
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
          syncUsage();
        });
        updateConnectionControls();
        return connectionRefresh;
      }

      async function refreshConnectionData() {
        stopUsage();
        let connected = false;
        let oauthSelected = false;
        let planUsageEnabled = false;
        let statusAvailable = false;
        let localURL = '';
        localConnectionAddress.hidden = true;
        localConnectionURL.textContent = '';
        localConnectionConfigError.hidden = true;

        let profilesAvailable = false;
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
          localURL = typeof status.local_url === 'string' ? status.local_url : '';
          statusAvailable = true;
        } catch (_) {}

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

        document.getElementById('account-select-label').hidden = accountSelect.hidden;
        connectionAvailable = statusAvailable && profilesAvailable;
        localConnectionURL.textContent = localURL;
        localConnectionAddress.hidden = localHost || !localURL;
        localConnectionConfigError.hidden = !statusAvailable || Boolean(localURL);
        const pendingProfile = selectedProfile?.registration_pending || (!activeProfileID && pendingProfiles.length > 0);
        const ready = connectionAvailable && connected && planUsageEnabled;
        const remoteSetup = document.getElementById('remote-connection-setup');
        const remoteHelp = document.getElementById('remote-connection-help');
        const setupSlot = document.getElementById(ready ? 'remote-connection-help-slot' : 'remote-connection-setup-slot');
        if (remoteSetup.parentElement !== setupSlot) setupSlot.appendChild(remoteSetup);
        remoteSetup.hidden = localHost || !connectionAvailable;
        remoteHelp.hidden = localHost || !ready;
        document.getElementById('remote-connection-heading').hidden = ready;
        document.getElementById('remote-connection-intro').hidden = ready;
        connectionStatus.hidden = ready || (!localHost && connectionAvailable);
        connectionStateLabel = !connectionAvailable ? 'Unavailable' : ready ? 'Connected' : pendingProfile ? 'Awaiting sign-in' : connected ? 'Permission required' : oauthSelected ? 'Sign-in required' : 'Not connected';
        connectionBadge.className = 'connection-badge' + (ready ? '' : (!connectionAvailable || pendingProfile || connected || oauthSelected) ? ' warning' : ' off');
        connectionStatus.textContent = !connectionAvailable
          ? 'Couldn’t check the ChatGPT connection. Select the status pill above to try again. Your saved accounts have not been removed.'
          : (connected && planUsageEnabled
            ? 'Active account: ' + (selectedProfile?.label || 'ChatGPT account') + '.'
            : (connected
              ? 'Review ChatGPT permissions to enable plan access.'
              : (localHost
                ? 'Connect your ChatGPT account to Agent.'
                : 'To connect, open Agent’s local address on the computer running it.')));
        if (pendingProfile && connectionAvailable) connectionStatus.textContent = localHost
          ? 'Select an incomplete account to finish sign-in.'
          : 'To finish sign-in, open Agent’s local address on the computer running it.';
        connectButton.hidden = !localHost || ready || !connectionAvailable;
        connectButton.textContent = pendingProfile ? 'Resume sign-in ↗' : connected ? 'Review ChatGPT permissions ↗' : oauthSelected ? 'Reconnect with ChatGPT ↗' : 'Continue with ChatGPT ↗';
        document.getElementById('remote-connection-action').textContent = 'Open Settings, then choose ' + (ready ? 'Add account' : connectButton.textContent.replace(' ↗', '')) + '.';
        addAccountButton.hidden = !localHost || profileCount === 0 || !connectionAvailable;
        disconnectButton.hidden = !oauthSelected || !selectedProfile || !connectionAvailable;
        const providerActions = document.getElementById('provider-actions');
        providerActions.classList.toggle('with-guidance', !remoteHelp.hidden);
        providerActions.hidden = remoteHelp.hidden && [connectButton, addAccountButton, disconnectButton].every(button => button.hidden);
        if (!localHost && connectionAvailable && ready) connectionStatus.textContent = 'Add or reconnect from Agent’s local address on the computer running it.';
        if (!localHost && connectionAvailable && connected && !planUsageEnabled) connectionStatus.textContent = 'To review permissions, open Agent’s local address on the computer running it.';

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
          if (ready) {
            connectionStatus.hidden = false;
            connectionStatus.textContent = 'Connected to ChatGPT, but its model list could not be loaded.';
          }
        }
      }

      copyLocalConnectionURL.addEventListener('click', async function () {
        const address = localConnectionURL.textContent;
        if (localConnectionAddress.hidden || !address) return;
        try {
          await navigator.clipboard.writeText(address);
          connectionFeedback('Local address copied.');
        } catch (_) {
          connectionFeedback('Copy unavailable. Select and copy the local address manually.', true);
        }
      });

      checkConnectionButton.addEventListener('click', async function () {
        if (connectionBusy || connectionRefresh) return;
        providerError.hidden = true;
        connectionFeedback('');
        await refreshConnectionState();
        if (connectionAvailable) connectionFeedback('Connection status refreshed.');
        else { providerError.textContent = 'Could not check the connection. Please try again. Your saved accounts have not been removed.'; providerError.hidden = false; }
      });

      function confirmConnectionChange(change) {
        if (connectionBusy || connectionRefresh) return;
        providerError.hidden = true;
        connectionFeedback('');
        connectionChange = change;
        connectionPassword.value = '';
        connectionConfirmError.hidden = true;
        connectionConfirmError.textContent = '';
        document.getElementById('disconnect-title').textContent = change.title;
        document.getElementById('disconnect-account').textContent = change.label;
        document.getElementById('disconnect-description').textContent = change.action === 'disconnect'
          ? 'Your saved conversations and subscription are kept.'
          : 'Confirm with your Agent password, not your ChatGPT password.';
        const confirm = document.getElementById('confirm-disconnect');
        confirm.textContent = change.button;
        confirm.className = change.action === 'disconnect' ? 'reset danger' : 'send';
        connectionBusy = true;
        updateConnectionControls();
        disconnectDialog.showModal();
        if (change.action !== 'disconnect') connectionPassword.focus();
      }

      async function startChatGPTAuthorization(path, trigger) {
        if (!localHost || connectionBusy || connectionRefresh) return;
        const expectedActive = activeProfileID;
        providerError.hidden = true;
        connectionFeedback('Opening ChatGPT sign-in…');
        connectionBusy = true;
        updateConnectionControls();
        let leaving = false;
        try {
          const response = await fetch(path, {
            method: 'POST', headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
            body: JSON.stringify({ expected_active_profile_id: expectedActive })
          });
          let result = {};
          if ((response.headers.get('content-type') || '').includes('application/json')) result = await response.json();
          if (!response.ok) throw new Error(result.error || 'ChatGPT sign-in could not be started. Please try again.');
          if (typeof result.authorization_url !== 'string' || !result.authorization_url) throw new Error('ChatGPT did not return a sign-in address. Please try again.');
          window.location.assign(result.authorization_url);
          leaving = true;
        } catch (failure) {
          connectionFeedback('');
          providerError.textContent = failure.message || 'ChatGPT sign-in could not be started. Please try again.';
          providerError.hidden = false;
          await refreshConnectionState();
        } finally {
          if (!leaving) {
            connectionBusy = false;
            updateConnectionControls();
            if (!settingsPanel.hidden) {
              const target = [trigger, connectButton, checkConnectionButton].find(control => control && !control.disabled && control.getClientRects().length);
              if (target) target.focus();
            }
          }
        }
      }

      connectButton.addEventListener('click', function () {
        if (!localHost || connectionBusy || connectionRefresh) return;
        if (!activeProfileID && pendingProfiles.length > 1) {
          providerError.textContent = 'Select the incomplete ChatGPT account above to resume its sign-in.';
          providerError.hidden = false;
          accountSelect.focus();
          return;
        }
        void startChatGPTAuthorization('/auth/start', connectButton);
      });

      addAccountButton.addEventListener('click', function () {
        if (!localHost || connectionBusy || connectionRefresh) return;
        void startChatGPTAuthorization('/auth/start?mode=add', addAccountButton);
      });

      accountSelect.addEventListener('change', function () {
        if (connectionBusy || connectionRefresh) return;
        const profileID = accountSelect.value;
        const option = accountSelect.selectedOptions[0];
        accountSelect.value = activeProfileID;
        if (!profileID || profileID === activeProfileID) return;
        if (option?.dataset.registrationPending === 'true') {
          if (!localHost) {
            providerError.textContent = 'To finish sign-in, open Agent’s local address on the computer running it.';
            providerError.hidden = false;
            return;
          }
          void startChatGPTAuthorization('/auth/start?mode=retry&profile_id=' + encodeURIComponent(profileID), accountSelect);
          return;
        }
        confirmConnectionChange({ action: 'switch', profileID: profileID, title: 'Switch ChatGPT account?',
          label: option.textContent, button: 'Switch account', trigger: accountSelect });
      });

      disconnectButton.addEventListener('click', function () {
        if (connectionBusy || connectionRefresh || !selectedProfile) return;
        confirmConnectionChange({ action: 'disconnect', profileID: selectedProfile.id, title: 'Disconnect ChatGPT?',
          label: 'Disconnect ' + (selectedProfile.label || 'ChatGPT account') + (selectedProfile.email ? ' · ' + selectedProfile.email : '') + ' from Agent?',
          button: 'Disconnect account', trigger: disconnectButton });
      });
      document.getElementById('cancel-disconnect').addEventListener('click', function () { if (!connectionSubmitting) disconnectDialog.close(); });
      disconnectDialog.addEventListener('cancel', function (event) { if (connectionSubmitting) event.preventDefault(); });
      disconnectDialog.addEventListener('close', function () {
        const trigger = connectionChange?.trigger;
        connectionChange = null;
        connectionPassword.value = '';
        connectionConfirmError.textContent = '';
        connectionConfirmError.hidden = true;
        accountSelect.value = activeProfileID;
        connectionBusy = false;
        updateConnectionControls();
        if (!settingsPanel.hidden && trigger && !trigger.hidden) trigger.focus();
      });
      connectionConfirmForm.addEventListener('submit', async function (event) {
        event.preventDefault();
        if (!disconnectDialog.open || !connectionChange || connectionSubmitting) return;
        const change = connectionChange;
        connectionSubmitting = true;
        connectionConfirmError.hidden = true;
        const controls = Array.from(connectionConfirmForm.elements);
        const payload = { password: connectionPassword.value };
        connectionPassword.value = '';
        payload.profile_id = change.profileID;
        controls.forEach(control => { control.disabled = true; });
        connectionConfirmForm.setAttribute('aria-busy', 'true');
        const confirm = document.getElementById('confirm-disconnect');
        confirm.textContent = 'Confirming…';
        try {
          const response = await fetch(change.action === 'switch' ? '/api/auth/profiles/active' : '/api/auth/disconnect', {
            method: 'POST', headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
            body: JSON.stringify(payload)
          });
          let result = {};
          if ((response.headers.get('content-type') || '').includes('application/json')) result = await response.json();
          if (!response.ok) {
            const failure = new Error(result.error || (response.status === 409 ? 'The active ChatGPT account changed. Review the account and try again.' : change.action === 'disconnect' ? 'The ChatGPT connection could not be removed. Please try again.' : 'Your ChatGPT connection could not be changed. Please try again.'));
            failure.passwordConfirmation = response.status === 401 || response.status === 429;
            throw failure;
          }

          disconnectDialog.close();
          await refreshConnectionState();
          if (change.action === 'switch') connectionFeedback('Active ChatGPT account changed.');
          else connectionFeedback(result.remote_revocation_confirmed
            ? 'Disconnected from ChatGPT on this host.'
            : 'Tokens were cleared here, but remote revocation was not confirmed. Revoke this app in ChatGPT settings.', !result.remote_revocation_confirmed);
        } catch (failure) {
          if (failure.passwordConfirmation) {
            connectionConfirmError.textContent = failure.message;
            connectionConfirmError.hidden = false;
          } else {
            disconnectDialog.close();
            providerError.textContent = failure.message || 'The ChatGPT connection could not be changed. Please try again.';
            providerError.hidden = false;
            await refreshConnectionState();
          }
        } finally {
          payload.password = '';
          connectionPassword.value = '';
          connectionSubmitting = false;
          controls.forEach(control => { control.disabled = false; });
          connectionConfirmForm.setAttribute('aria-busy', 'false');
          confirm.textContent = change.button;
          connectionBusy = disconnectDialog.open;
          updateConnectionControls();
          if (disconnectDialog.open) connectionPassword.focus();
          else if (!settingsPanel.hidden) {
            const target = [change.trigger, connectButton, checkConnectionButton].find(control => control && !control.disabled && control.getClientRects().length);
            if (target) target.focus();
          }
        }
      });

      modelSelect.addEventListener('change', function () {
        try { localStorage.setItem(modelStorageKey, modelSelect.value); } catch (_) {}
      });

      void requireSignIn();
      window.addEventListener('pageshow', function (event) {
        // Back navigation may restore the page with its outgoing sign-in lock.
        if (event.persisted && !disconnectDialog.open) {
          connectionBusy = false;
          void requireSignIn();
        }
      });

      const researchToggle = document.getElementById('research-toggle');
      const researchConsent = document.getElementById('research-consent');
      const researchApprove = document.getElementById('research-approve');
      const researchRetry = document.getElementById('research-retry');
      const researchFeedback = document.getElementById('research-feedback');
      let researchPending = null;
      let activeResearch = null;
      const researchScope = document.getElementById('research-scope');
      const researchStop = document.getElementById('research-stop');
      const researchRevoke = document.getElementById('research-revoke');
      const researchPermissions = document.getElementById('research-permissions');
      const researchDetails = document.getElementById('research-permission-details');
      const researchControlFeedback = document.getElementById('research-control-feedback');
      researchPermissions.addEventListener('click', function () {
        researchDetails.hidden = !researchDetails.hidden;
        researchPermissions.setAttribute('aria-expanded', String(!researchDetails.hidden));
      });
      async function endResearch(action) {
        const state = activeResearch;
        if (!state || state.controlBusy) return;
        state.controlBusy = true;
        researchStop.disabled = researchRevoke.disabled = true;
        researchControlFeedback.textContent = action === 'revoke' ? 'Requesting revocation…' : 'Requesting stop…';
        try {
          const result = await researchAPI('/api/research/tasks/' + encodeURIComponent(state.id) + '/' + action, {});
          if (activeResearch !== state) return;
          if (result.task?.status !== (action === 'revoke' ? 'revoked' : 'cancelled')) throw new Error('Server did not confirm the request. Try again.');
          state.terminal = action === 'revoke' ? 'Research permission revoked.' : 'Research stopped.';
          state.controller.abort();
        } catch (error) {
          if (activeResearch !== state) return;
          researchControlFeedback.textContent = error.message + ' Research may still be running; retry Stop or Revoke.';
        } finally {
          state.controlBusy = false;
          if (activeResearch === state) researchStop.disabled = researchRevoke.disabled = false;
        }
      }
      function requestEndResearch(action) {
        const state = activeResearch;
        if (state && !state.controlBusy) state.controlPromise = endResearch(action);
      }
      researchStop.addEventListener('click', function () { requestEndResearch('stop'); });
      researchRevoke.addEventListener('click', function () { requestEndResearch('revoke'); });
      async function researchAPI(path, payload) {
        const controller = new AbortController();
        const timer = setTimeout(() => controller.abort(), 10000);
        try {
          const response = await fetch(path, { method: payload === undefined ? 'GET' : 'POST', cache: 'no-store', signal: controller.signal,
            headers: { 'Content-Type': 'application/json' }, body: payload === undefined ? undefined : JSON.stringify(payload) });
          const result = await response.json();
          if (!response.ok) throw new Error(result.error || 'Research permission request failed. Please try again.');
          return result;
        } catch (error) {
          if (controller.signal.aborted) throw new Error('Research permission request timed out. Please retry.');
          throw error;
        } finally { clearTimeout(timer); }
      }
      async function finishResearchConsent(state, choice) {
        if (researchPending !== state || state.cancelling || state.replacing) return;
        if (choice === null || choice === 'declined') {
          state.approvalVersion++;
          state.cancelling = true;
          researchApprove.disabled = true;
          try {
            if (state.task) {
              const result = await researchAPI('/api/research/tasks/' + encodeURIComponent(state.task.id) + '/revoke', {});
              if (!['revoked','cancelled','expired','failed','completed'].includes(result.task?.status)) throw new Error('Permission revocation was not confirmed.');
            }
          } catch (error) {
            if (researchPending === state) researchFeedback.textContent = error.message + ' No model request was sent. Retry Cancel or Answer without browsing to close this permission.';
            state.cancelling = false;
            return;
          }
          state.cancelling = false;
          if (researchPending !== state) return;
        }
        researchPending = null;
        researchConsent.hidden = true;
        form.hidden = false;
        state.resolve(choice);
      }
      async function prepareResearchConsent(state) {
        if (researchPending !== state || state.preparing || state.cancelling || state.approving) return;
        state.preparing = true;
        state.replacing = !!state.task;
        state.approvalVersion++;
        document.getElementById('research-cancel').disabled = document.getElementById('research-decline').disabled = state.replacing;
        researchApprove.disabled = true;
        researchRetry.hidden = true;
        researchFeedback.textContent = 'Preparing a task-scoped permission request. No browsing has started.';
        document.getElementById('research-provider').textContent = 'Checking the configured search provider…';
        try {
          if (state.task) {
            const task = state.task;
            const result = await researchAPI('/api/research/tasks/' + encodeURIComponent(task.id) + '/revoke', {});
            if (researchPending !== state || state.task !== task) return;
            if (!['revoked','cancelled','expired','failed','completed'].includes(result.task?.status)) throw new Error('Permission revocation was not confirmed.');
            // Replace an immutable identity only after its grant is terminal.
            state.task = null;
          }
          const config = await researchAPI('/api/research');
          if (researchPending !== state) return;
          if (!config.available) throw new Error(config.message || 'Public web research is unavailable.');
          const result = await researchAPI('/api/research/tasks', state.request);
          if (researchPending !== state) return;
          state.task = result.task;
          if (!state.task?.id || state.task.status !== 'pending' || state.task.scope !== 'public_web') throw new Error('Task permission could not be prepared.');
          document.getElementById('research-provider').textContent = state.task.search_available
            ? 'Search provider: ' + state.task.search_provider + '. Public pages only.'
            : 'Public-page retrieval only. Global web search is not configured.';
          researchFeedback.textContent = 'Permission applies to this request only and expires shortly. Nothing runs until you allow it.';
          researchApprove.disabled = false;
        } catch (error) {
          if (researchPending !== state) return;
          researchFeedback.textContent = error.message;
          researchRetry.hidden = false;
        } finally {
          state.preparing = state.replacing = false;
          if (researchPending === state) document.getElementById('research-cancel').disabled = document.getElementById('research-decline').disabled = false;
        }
      }
      function requestResearchConsent(request) {
        return new Promise(function (resolve) {
          const state = { request: request, resolve: resolve, task: null, approving: false, approvalVersion: 0 };
          researchPending = state;
          form.hidden = true;
          document.getElementById('research-prompt').parentElement.open = true;
          document.querySelector('.research-consent-copy').scrollTop = 0;
          document.getElementById('research-prompt').textContent = request.messages[request.messages.length - 1].content;
          researchConsent.hidden = false;
          document.getElementById('research-cancel').focus();
          void prepareResearchConsent(state);
        });
      }
      researchApprove.addEventListener('click', async function () {
        const state = researchPending;
        if (!state?.task || state.approving || state.preparing || state.cancelling || researchApprove.disabled) return;
        const task = state.task;
        const approvalVersion = state.approvalVersion;
        state.approving = true;
        researchApprove.disabled = true;
        researchFeedback.textContent = 'Confirming permission…';
        try {
          const result = await researchAPI('/api/research/tasks/' + encodeURIComponent(task.id) + '/approve', {});
          if (researchPending !== state || state.task !== task || state.approvalVersion !== approvalVersion) return;
          if (result.task?.status !== 'approved') throw new Error('Permission was not approved. Review and retry.');
          finishResearchConsent(state, task.id);
        } catch (error) {
          if (researchPending !== state || state.task !== task || state.approvalVersion !== approvalVersion) return;
          researchFeedback.textContent = error.message + ' No model request was sent. Retry to review a new permission request.';
          // A failed acknowledgment does not prove the server rejected approval.
          researchRetry.hidden = false;
          researchRetry.focus();
        } finally { state.approving = false; }
      });
      researchRetry.addEventListener('click', function () {
        if (researchPending && !researchPending.approving) void prepareResearchConsent(researchPending);
      });
      document.getElementById('research-decline').addEventListener('click', function () {
        if (researchPending) finishResearchConsent(researchPending, 'declined');
      });
      document.getElementById('research-cancel').addEventListener('click', function () {
        if (researchPending) finishResearchConsent(researchPending, null);
      });
      researchConsent.addEventListener('keydown', function (event) {
        if (event.key === 'Escape') { event.preventDefault(); document.getElementById('research-cancel').click(); }
      });

      form.addEventListener('submit', async function (event) {
        event.preventDefault();
        const content = input.value.trim();
        if (!content || busy || loadingConversation) return;
        const version = conversationVersion;
        const runID = newRunID();
        const requestMessages = conversation.slice(-39).concat([{ role: 'user', content: content }]);
        const request = Object.freeze({ messages: Object.freeze(requestMessages.map(message => Object.freeze(Object.assign({}, message)))), model: modelSelect.value, conversation_id: conversationId, run_id: runID, omitted_messages: omittedConversationMessages + Math.max(0, conversation.length - 39) });
        let researchTaskID = '';
        const optedIn = researchToggle.checked;
        researchToggle.checked = false;
        if (optedIn) {
          researchModelWasDisabled = modelSelect.disabled;
          researchFlow = true;
          busy = true;
          setInteractionState();
          input.disabled = true;
          const choice = await requestResearchConsent(request);
          input.disabled = false;
          if (!choice) { researchFlow = false; modelSelect.disabled = researchModelWasDisabled; busy = false; setInteractionState(); input.focus(); return; }
          if (choice !== 'declined') researchTaskID = choice;
        }

        const welcome = document.getElementById('welcome');
        if (welcome) welcome.remove();
        addMessage('user', content);
        const thinking = addMessage('assistant', 'Thinking…');
        pendingRuns.add(runID);
        attachRun(thinking, runID);
        input.value = '';
        busy = true;
        setInteractionState();
        setConversationNotice('Waiting for the model…');
        notice.classList.remove('error');
        const researchExecution = researchTaskID ? { id: researchTaskID, controller: new AbortController(), terminal: '', controlBusy: false } : null;
        if (researchExecution) {
          activeResearch = researchExecution;
          researchScope.dataset.taskId = researchExecution.id;
          researchScope.hidden = false;
          researchDetails.hidden = true;
          researchPermissions.setAttribute('aria-expanded', 'false');
          researchStop.disabled = researchRevoke.disabled = false;
          researchControlFeedback.textContent = '';
        }

        try {
          const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            signal: researchExecution?.controller.signal,
            body: JSON.stringify(researchTaskID ? Object.assign({}, request, { research_task_id: researchTaskID }) : request)
          });
          const result = await response.json();
          if (researchExecution?.controlPromise) await researchExecution.controlPromise;
          if (researchExecution?.terminal) throw new Error(researchExecution.terminal);
          if (version !== conversationVersion) return;
          if (!response.ok) {
            const requestError = new Error(result.error || 'The request failed.');
            requestError.action = result.action;
            throw requestError;
          }
          thinking.remove();
          conversationId = result.conversation_id || conversationId;
          if (conversationTitle.textContent === 'New conversation') conversationTitle.textContent = content.slice(0, 80);
          attachRun(addMessage('assistant', result.reply), result.run_id || runID);
          conversation.push({ role: 'user', content: content }, { role: 'assistant', content: result.reply });
          if (conversation.length > 40) omittedConversationMessages += conversation.splice(0, conversation.length - 40).length;
          setConversationNotice('Reply received. This conversation has not been saved.', Boolean(conversationId));
          await refreshConversations();
        } catch (error) {
          if (researchExecution?.controlPromise) await researchExecution.controlPromise;
          if (version !== conversationVersion) return;
          thinking.remove();
          if (researchExecution?.terminal) {
            attachRun(addMessage('assistant', researchExecution.terminal), runID);
            setConversationNotice(researchExecution.terminal + ' Previous outbound requests cannot be undone.');
          } else {
            attachRun(addMessage('assistant', 'I could not complete that request.'), runID);
            showError(error.message || 'Check the model provider settings and try again.', error.action);
            if (error.action === 'reauthorize') await refreshConnectionState();
          }
        } finally {
          if (researchExecution) {
            activeResearch = null;
            researchScope.hidden = true;
            delete researchScope.dataset.taskId;
          }
          pendingRuns.delete(runID);
          if (inspectedRun === runID) { clearTimeout(runTimer); if (runController) { stopRunPolling(); } void readRun(); }
          if (optedIn) { researchFlow = false; modelSelect.disabled = researchModelWasDisabled; }
          busy = false;
          setInteractionState();
          if (settingsPanel.hidden && runPanel.hidden) input.focus();
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
        researchToggle.checked = false;
        showSettings(false);
        conversationVersion++;
        closeRunInspector(false);
        omittedConversationMessages = 0;
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
        setConversationNotice('Messages are saved to your account after each reply.');
        notice.classList.remove('error');
        input.focus();
      });
    })();
  </script>
</body>
</html>`
