package main

const homePage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light">
  <title>EveryoneAgent</title>
  <style>
    :root { color-scheme: light; font: 16px/1.5 system-ui, sans-serif; color: #29252b; background: #f4f1ed; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; }
    .app { width: min(54rem, 100%); height: 100vh; height: 100dvh; margin: 0 auto; display: flex; flex-direction: column; background: #fff; border-inline: 1px solid #e9e4df; }
    header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 1rem 1.25rem; border-bottom: 1px solid #eee9e4; }
    .brand { color: #6d4a94; font-size: .82rem; font-weight: 750; letter-spacing: .09em; }
    .subtitle { margin: .15rem 0 0; color: #716b72; font-size: .82rem; }
    button, textarea { font: inherit; }
    button { border: 0; border-radius: .7rem; cursor: pointer; }
    button:focus-visible, textarea:focus-visible { outline: 3px solid #bda4da; outline-offset: 2px; }
    .reset { padding: .55rem .8rem; color: #5c426f; background: #f3edf8; font-size: .88rem; }
    .messages { flex: 1; overflow-y: auto; padding: 1.5rem clamp(1rem, 5vw, 3rem); scroll-behavior: smooth; }
    .welcome { max-width: 36rem; margin: 12vh auto 0; text-align: center; }
    .welcome h1 { margin: 0 0 .5rem; font-size: clamp(1.7rem, 5vw, 2.3rem); letter-spacing: -.04em; }
    .welcome p { margin: .4rem 0; color: #716b72; }
    .message { width: fit-content; max-width: min(85%, 42rem); margin: 1rem 0; padding: .85rem 1rem; border-radius: 1rem; white-space: pre-wrap; overflow-wrap: anywhere; }
    .message.user { margin-left: auto; background: #f2edf7; border-bottom-right-radius: .3rem; }
    .message.assistant { background: #f7f6f4; border-bottom-left-radius: .3rem; }
    .sender { margin-bottom: .25rem; color: #746b78; font-size: .72rem; font-weight: 700; letter-spacing: .04em; text-transform: uppercase; }
    .message-text { line-height: 1.6; }
    .visually-hidden { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
    .composer-wrap { padding: .75rem clamp(.75rem, 3vw, 1.25rem) 1rem; border-top: 1px solid #eee9e4; }
    .notice { min-height: 1.35rem; margin: 0 0 .5rem; color: #746b78; font-size: .78rem; }
    .notice.error { color: #a23e36; }
    .composer { display: flex; align-items: flex-end; gap: .65rem; padding: .6rem; border: 1px solid #ddd6df; border-radius: 1rem; background: #fff; box-shadow: 0 .4rem 1.4rem #30241b0d; }
    textarea { flex: 1; min-height: 2.75rem; max-height: 10rem; resize: vertical; padding: .65rem .7rem; border: 0; outline: 0; color: inherit; background: transparent; }
    textarea::placeholder { color: #928b94; }
    .send { min-width: 4.4rem; padding: .68rem .9rem; color: #fff; background: #6d4a94; font-weight: 650; }
    .send:hover:not(:disabled) { background: #583878; }
    .send:disabled { cursor: wait; opacity: .55; }
    .hint { margin: .5rem 0 0; color: #8b848c; font-size: .72rem; text-align: center; }
    @media (max-width: 520px) {
      .app { border: 0; }
      header { padding: .8rem 1rem; }
      .messages { padding: 1rem; }
      .welcome { margin-top: 8vh; }
      .message { max-width: 94%; }
    }
  </style>
</head>
<body>
  <main class="app">
    <header>
      <div>
        <div class="brand">EVERYONEAGENT</div>
        <p class="subtitle">A private chat for your household</p>
      </div>
      <button class="reset" id="new-chat" type="button">New chat</button>
    </header>
    <section class="messages" id="messages" aria-live="polite" aria-relevant="additions text" aria-label="Conversation">
      <div class="welcome" id="welcome">
        <h1>What can I help with?</h1>
        <p>Ask a question to start a conversation.</p>
      </div>
    </section>
    <div class="composer-wrap">
      <p class="notice" id="notice" role="status">Messages are sent to your configured model provider and are not saved by this app yet.</p>
      <form class="composer" id="chat-form" autocomplete="off">
        <label class="visually-hidden" for="message-input">Message</label>
        <textarea id="message-input" name="message" rows="1" maxlength="8192" placeholder="Message EveryoneAgent" required></textarea>
        <button class="send" id="send-button" type="submit">Send</button>
      </form>
      <p class="hint">Enter to send · Shift+Enter for a new line</p>
    </div>
  </main>
  <script>
    (function () {
      const form = document.getElementById('chat-form');
      const input = document.getElementById('message-input');
      const sendButton = document.getElementById('send-button');
      const messageList = document.getElementById('messages');
      const notice = document.getElementById('notice');
      const conversation = [];
      let busy = false;

      function addMessage(role, content) {
        const bubble = document.createElement('article');
        bubble.className = 'message ' + role;
        const sender = document.createElement('div');
        sender.className = 'sender';
        sender.textContent = role === 'user' ? 'You' : 'EveryoneAgent';
        const text = document.createElement('div');
        text.className = 'message-text';
        text.textContent = content;
        bubble.append(sender, text);
        messageList.appendChild(bubble);
        messageList.scrollTop = messageList.scrollHeight;
        return bubble;
      }

      function showError(message) {
        notice.textContent = message;
        notice.classList.add('error');
      }

      form.addEventListener('submit', async function (event) {
        event.preventDefault();
        const content = input.value.trim();
        if (!content || busy) return;

        const welcome = document.getElementById('welcome');
        if (welcome) welcome.remove();
        addMessage('user', content);
        const requestMessages = conversation.slice(-39).concat([{ role: 'user', content: content }]);
        const thinking = addMessage('assistant', 'Thinking…');
        input.value = '';
        busy = true;
        sendButton.disabled = true;
        notice.textContent = 'Waiting for the model…';
        notice.classList.remove('error');

        try {
          const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ messages: requestMessages })
          });
          const result = await response.json();
          if (!response.ok) throw new Error(result.error || 'The request failed.');
          thinking.remove();
          addMessage('assistant', result.reply);
          conversation.push({ role: 'user', content: content }, { role: 'assistant', content: result.reply });
          if (conversation.length > 40) conversation.splice(0, conversation.length - 40);
          notice.textContent = 'This conversation is in this browser tab only; it is not saved yet.';
        } catch (error) {
          thinking.remove();
          addMessage('assistant', 'I could not complete that request.');
          showError(error.message || 'Check the model provider settings and try again.');
        } finally {
          busy = false;
          sendButton.disabled = false;
          input.focus();
        }
      });

      input.addEventListener('keydown', function (event) {
        if (event.key === 'Enter' && !event.shiftKey) {
          event.preventDefault();
          form.requestSubmit();
        }
      });

      document.getElementById('new-chat').addEventListener('click', function () {
        if (busy) return;
        conversation.length = 0;
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
        notice.textContent = 'Messages are sent to your configured model provider and are not saved by this app yet.';
        notice.classList.remove('error');
        input.focus();
      });
    })();
  </script>
</body>
</html>`
