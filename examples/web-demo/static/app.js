const API_BASE = '/api';

let sessionId = generateSessionId();
let isStreaming = false;
let messages = [];
let providerModels = {};

const providerSelect = document.getElementById('provider');
const modelSelect = document.getElementById('model');
const apiKeyInput = document.getElementById('apiKey');
const messageInput = document.getElementById('messageInput');
const sendBtn = document.getElementById('sendBtn');
const newChatBtn = document.getElementById('newChatBtn');
const messagesContainer = document.getElementById('messagesContainer');
const charCount = document.getElementById('charCount');
const connectionStatus = document.getElementById('connectionStatus');
const toolsList = document.getElementById('toolsList');
const menuBtn = document.getElementById('menuBtn');
const sidebar = document.getElementById('sidebar');
const sidebarOverlay = document.getElementById('sidebarOverlay');

function generateSessionId() {
    return 'session-' + Date.now() + '-' + Math.random().toString(36).substr(2, 9);
}

function init() {
    loadModels();
    loadSettings();
    loadTools();
    setupEventListeners();
    updateInputState();
}

async function loadModels() {
    try {
        const response = await fetch(`${API_BASE}/models`);
        const data = await response.json();
        
        providerModels = {};
        if (data.models) {
            data.models.forEach(model => {
                if (!providerModels[model.provider]) {
                    providerModels[model.provider] = [];
                }
                providerModels[model.provider].push(model.model_id);
            });
        }
        
        updateModelOptions();
    } catch (error) {
        console.error('Failed to load models:', error);
        providerModels = {
            openai: ['gpt-4o', 'gpt-4o-mini'],
            anthropic: ['claude-3.5-sonnet', 'claude-3-haiku'],
            gemini: ['gemini-1.5-pro', 'gemini-1.5-flash'],
            zai: ['zai-1']
        };
        updateModelOptions();
    }
}

function loadSettings() {
    const savedProvider = localStorage.getItem('rl-agent-provider');
    const savedModel = localStorage.getItem('rl-agent-model');
    const savedApiKey = localStorage.getItem('rl-agent-api-key');
    
    if (savedProvider) providerSelect.value = savedProvider;
    if (savedModel) modelSelect.value = savedModel;
    if (savedApiKey) apiKeyInput.value = savedApiKey;
}

function saveSettings() {
    localStorage.setItem('rl-agent-provider', providerSelect.value);
    localStorage.setItem('rl-agent-model', modelSelect.value);
    localStorage.setItem('rl-agent-api-key', apiKeyInput.value);
}

function updateModelOptions() {
    const provider = providerSelect.value;
    const models = providerModels[provider] || [];
    
    modelSelect.innerHTML = '';
    models.forEach(model => {
        const option = document.createElement('option');
        option.value = model;
        option.textContent = model;
        modelSelect.appendChild(option);
    });
}

async function loadTools() {
    try {
        const response = await fetch(`${API_BASE}/tools`);
        const data = await response.json();
        
        toolsList.innerHTML = '';
        data.tools.forEach(tool => {
            const toolItem = document.createElement('div');
            toolItem.className = 'tool-item';
            toolItem.innerHTML = `
                <div class="tool-name">${tool.name}</div>
                <div class="tool-desc">${tool.description}</div>
            `;
            toolsList.appendChild(toolItem);
        });
    } catch (error) {
        console.error('Failed to load tools:', error);
    }
}

function setupEventListeners() {
    providerSelect.addEventListener('change', () => {
        updateModelOptions();
        saveSettings();
    });
    
    modelSelect.addEventListener('change', saveSettings);
    apiKeyInput.addEventListener('input', () => {
        saveSettings();
        updateInputState();
    });
    
    messageInput.addEventListener('input', () => {
        autoResizeTextarea();
        updateCharCount();
    });
    
    messageInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            sendMessage();
        }
    });
    
    sendBtn.addEventListener('click', sendMessage);
    newChatBtn.addEventListener('click', () => {
        startNewChat();
        closeSidebar();
    });

    if (menuBtn) {
        menuBtn.addEventListener('click', toggleSidebar);
    }
    if (sidebarOverlay) {
        sidebarOverlay.addEventListener('click', closeSidebar);
    }
}

function toggleSidebar() {
    sidebar.classList.toggle('open');
    sidebarOverlay.classList.toggle('active');
}

function closeSidebar() {
    sidebar.classList.remove('open');
    sidebarOverlay.classList.remove('active');
}

function updateInputState() {
    const hasApiKey = apiKeyInput.value.trim().length > 0;
    messageInput.disabled = !hasApiKey || isStreaming;
    sendBtn.disabled = !hasApiKey || isStreaming || messageInput.value.trim().length === 0;
}

function updateCharCount() {
    charCount.textContent = messageInput.value.length;
    updateInputState();
}

function autoResizeTextarea() {
    messageInput.style.height = 'auto';
    messageInput.style.height = Math.min(messageInput.scrollHeight, 150) + 'px';
}

function startNewChat() {
    sessionId = generateSessionId();
    messages = [];
    messagesContainer.innerHTML = `
        <div class="welcome-message">
            <div class="welcome-icon">
                <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                    <path d="M12 2a10 10 0 1 0 10 10A10 10 0 0 0 12 2zm0 18a8 8 0 1 1 8-8 8 8 0 0 1-8 8z"></path>
                    <path d="M12 6v6l4 2"></path>
                </svg>
            </div>
            <h2>Welcome to RL-Agent</h2>
            <p>Configure your provider and API key to start chatting.</p>
            <div class="features-list">
                <div class="feature">
                    <span class="feature-icon">💬</span>
                    <span>Real-time streaming responses</span>
                </div>
                <div class="feature">
                    <span class="feature-icon">🔧</span>
                    <span>Tool calling support</span>
                </div>
                <div class="feature">
                    <span class="feature-icon">🔒</span>
                    <span>API keys stored locally</span>
                </div>
            </div>
        </div>
    `;
    messageInput.value = '';
    updateCharCount();
}

function addMessage(role, content, toolCalls = []) {
    const welcomeMessage = messagesContainer.querySelector('.welcome-message');
    if (welcomeMessage) {
        welcomeMessage.remove();
    }
    
    const messageDiv = document.createElement('div');
    messageDiv.className = `message ${role}`;
    
    const avatar = role === 'user' ? 'U' : 'AI';
    
    let toolCallsHtml = '';
    if (toolCalls.length > 0) {
        toolCallsHtml = `
            <div class="tool-calls">
                ${toolCalls.map(tc => `
                    <div class="tool-badge">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"></path>
                        </svg>
                        ${tc.name}
                    </div>
                `).join('')}
            </div>
        `;
    }
    
    messageDiv.innerHTML = `
        <div class="message-avatar">${avatar}</div>
        <div class="message-content">
            <div class="message-bubble">
                <div class="message-text">${formatContent(content)}</div>
                ${toolCallsHtml}
            </div>
            <div class="message-meta">${new Date().toLocaleTimeString()}</div>
        </div>
    `;
    
    messagesContainer.appendChild(messageDiv);
    scrollToBottom();
    
    return messageDiv;
}

function addStreamingMessage(role) {
    const welcomeMessage = messagesContainer.querySelector('.welcome-message');
    if (welcomeMessage) {
        welcomeMessage.remove();
    }
    
    const messageDiv = document.createElement('div');
    messageDiv.className = `message ${role}`;
    messageDiv.id = 'streaming-message';
    
    const avatar = role === 'user' ? 'U' : 'AI';
    
    messageDiv.innerHTML = `
        <div class="message-avatar">${avatar}</div>
        <div class="message-content">
            <div class="message-bubble">
                <div class="message-text"></div>
                <div class="tool-calls"></div>
            </div>
            <div class="message-meta">${new Date().toLocaleTimeString()}</div>
        </div>
    `;
    
    messagesContainer.appendChild(messageDiv);
    scrollToBottom();
    
    return messageDiv;
}

function updateStreamingMessage(content, toolCalls = []) {
    const messageDiv = document.getElementById('streaming-message');
    if (!messageDiv) return;
    
    const textDiv = messageDiv.querySelector('.message-text');
    const toolCallsDiv = messageDiv.querySelector('.tool-calls');
    
    textDiv.innerHTML = formatContent(content);
    
    if (toolCalls.length > 0) {
        toolCallsDiv.innerHTML = toolCalls.map(tc => `
            <div class="tool-badge ${tc.success ? 'success' : tc.error ? 'error' : ''}">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"></path>
                </svg>
                ${tc.name}
                ${tc.success ? ' ✓' : tc.error ? ' ✗' : ''}
            </div>
        `).join('');
    }
    
    scrollToBottom();
}

function finalizeStreamingMessage() {
    const messageDiv = document.getElementById('streaming-message');
    if (messageDiv) {
        messageDiv.id = '';
    }
}

function addTypingIndicator() {
    const messageDiv = document.createElement('div');
    messageDiv.className = 'message assistant';
    messageDiv.id = 'typing-indicator';
    
    messageDiv.innerHTML = `
        <div class="message-avatar">AI</div>
        <div class="message-content">
            <div class="message-bubble">
                <div class="typing-indicator">
                    <span></span>
                    <span></span>
                    <span></span>
                </div>
            </div>
        </div>
    `;
    
    messagesContainer.appendChild(messageDiv);
    scrollToBottom();
}

function removeTypingIndicator() {
    const indicator = document.getElementById('typing-indicator');
    if (indicator) {
        indicator.remove();
    }
}

function formatContent(content) {
    if (!content) return '';
    
    content = content
        .replace(/```(\w+)?\n([\s\S]*?)```/g, '<pre><code class="language-$1">$2</code></pre>')
        .replace(/`([^`]+)`/g, '<code>$1</code>')
        .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
        .replace(/\*([^*]+)\*/g, '<em>$1</em>')
        .replace(/\n/g, '<br>');
    
    return content;
}

function scrollToBottom() {
    messagesContainer.scrollTop = messagesContainer.scrollHeight;
}

async function sendMessage() {
    const content = messageInput.value.trim();
    if (!content || isStreaming) return;
    
    const apiKey = apiKeyInput.value.trim();
    if (!apiKey) {
        showError('Please enter an API key');
        return;
    }
    
    isStreaming = true;
    updateInputState();
    
    addMessage('user', content);
    messageInput.value = '';
    autoResizeTextarea();
    updateCharCount();
    
    addTypingIndicator();
    
    try {
        const response = await fetch(`${API_BASE}/stream`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                session_id: sessionId,
                provider: providerSelect.value,
                model: modelSelect.value,
                api_key: apiKey,
                message: content
            })
        });
        
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        
        removeTypingIndicator();
        
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        
        let fullContent = '';
        let toolCalls = [];
        let buffer = '';
        
        addStreamingMessage('assistant');
        
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            
            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop() || '';
            
            for (const line of lines) {
                if (line.startsWith('event: ')) {
                    continue;
                }
                
                if (line.startsWith('data: ')) {
                    const data = line.slice(6);
                    try {
                        const event = JSON.parse(data);
                        handleStreamEvent(event, (delta) => {
                            fullContent += delta;
                            updateStreamingMessage(fullContent, toolCalls);
                        }, (tc) => {
                            toolCalls.push(tc);
                            updateStreamingMessage(fullContent, toolCalls);
                        });
                    } catch (e) {
                        console.error('Failed to parse event:', e);
                    }
                }
            }
        }
        
        finalizeStreamingMessage();
        
    } catch (error) {
        removeTypingIndicator();
        showError(error.message);
        console.error('Stream error:', error);
    } finally {
        isStreaming = false;
        updateInputState();
    }
}

function handleStreamEvent(event, onUpdate, onToolCall) {
    switch (event.type || Object.keys(event)[0]) {
        case 'content_delta':
            if (event.delta) {
                onUpdate(event.delta);
            }
            break;
        case 'tool_call':
            if (event.name) {
                onToolCall({
                    name: event.name,
                    arguments: event.arguments
                });
            }
            break;
        case 'tool_result':
            if (event.tool_name) {
                onToolCall({
                    name: event.tool_name,
                    success: event.success,
                    result: event.result,
                    error: event.error
                });
            }
            break;
        case 'error':
            showError(event.error || 'An error occurred');
            break;
        case 'finished':
            break;
    }
}

function showError(message) {
    const existing = document.querySelector('.error-toast');
    if (existing) existing.remove();
    
    const toast = document.createElement('div');
    toast.className = 'error-toast';
    toast.textContent = message;
    document.body.appendChild(toast);
    
    setTimeout(() => {
        toast.remove();
    }, 5000);
}

function setConnectionStatus(connected) {
    connectionStatus.textContent = connected ? 'Connected' : 'Disconnected';
    connectionStatus.className = `status-badge ${connected ? 'connected' : 'disconnected'}`;
}

document.addEventListener('DOMContentLoaded', init);
