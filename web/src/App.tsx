import React, { useState, useRef, useEffect } from 'react';
import { Paperclip, ArrowUp, Plus } from 'lucide-react';
import ReactMarkdown from 'react-markdown';

interface Message {
  id: string;
  sender: 'user' | 'assistant';
  text: string;
  citations?: string[];
  isStreaming?: boolean;
}

interface IngestedDoc {
  id: string;
  filename: string;
  num_chunks: number;
  created_at: string;
}

export default function App() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [inputPrompt, setInputPrompt] = useState('');
  const [isIngesting, setIsIngesting] = useState(false);
  const [isStreaming, setIsStreaming] = useState(false);
  const [docs, setDocs] = useState<IngestedDoc[]>([]);
  const [uploadStatus, setUploadStatus] = useState<string | null>(null);

  const fileInputRef = useRef<HTMLInputElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, isStreaming]);

  const fetchDocs = async () => {
    try {
      const res = await fetch('/v1/documents');
      if (res.ok) {
        const json = await res.json();
        setDocs(json.data || []);
      }
    } catch (err) {
      console.error('Fetch docs error:', err);
    }
  };

  useEffect(() => {
    fetchDocs();
  }, []);

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    setIsIngesting(true);
    setUploadStatus(`Uploading ${file.name}...`);

    try {
      const formData = new FormData();
      formData.append('file', file);
      formData.append('filename', file.name);

      const res = await fetch('/v1/documents/ingest', {
        method: 'POST',
        body: formData,
      });

      const data = await res.json();

      if (res.ok) {
        setUploadStatus(`Uploaded ${file.name} (${data.data.inserted_count} chunks)`);
        fetchDocs();
        setTimeout(() => setUploadStatus(null), 3000);
      } else {
        setUploadStatus(`Error: ${data.error?.message || 'Failed to upload'}`);
      }
    } catch (err: any) {
      setUploadStatus(`Error: ${err.message}`);
    } finally {
      setIsIngesting(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  };

  const handleSendMessage = async (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    const prompt = inputPrompt.trim();
    if (!prompt || isStreaming) return;

    const userMsgId = 'user-' + Date.now();
    const assistantMsgId = 'assist-' + Date.now();

    const userMessage: Message = {
      id: userMsgId,
      sender: 'user',
      text: prompt,
    };

    const assistantMessage: Message = {
      id: assistantMsgId,
      sender: 'assistant',
      text: '',
      isStreaming: true,
    };

    setMessages((prev) => [...prev, userMessage, assistantMessage]);
    setInputPrompt('');
    setIsStreaming(true);

    try {
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          model: 'gemini-3.6-flash',
          messages: [{ role: 'user', content: prompt }],
          stream: true,
        }),
      });

      if (!response.ok) {
        const errJson = await response.json();
        throw new Error(errJson.error?.message || 'Server error');
      }

      const citationsHeader = response.headers.get('X-RAG-Citations') || '';
      const citations = citationsHeader ? citationsHeader.split('; ').filter(Boolean) : [];

      const reader = response.body?.getReader();
      const decoder = new TextDecoder('utf-8');
      let accumulatedText = '';
      let buffer = '';

      if (reader) {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;

          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split('\n');
          buffer = lines.pop() || '';

          for (const rawLine of lines) {
            const line = rawLine.trim();
            if (!line) continue;

            if (line.startsWith('data:')) {
              const dataStr = line.slice(5).trim();
              if (dataStr === '[DONE]') break;

              try {
                const parsed = JSON.parse(dataStr);
                const delta = parsed.choices[0]?.delta?.content || '';
                if (delta) {
                  accumulatedText += delta;
                  setMessages((prev) =>
                    prev.map((msg) =>
                      msg.id === assistantMsgId
                        ? { ...msg, text: accumulatedText, citations }
                        : msg
                    )
                  );
                }
              } catch (err) {
                // Ignore incomplete JSON chunks
              }
            }
          }
        }
      }

      if (!accumulatedText.trim()) {
        setMessages((prev) =>
          prev.map((msg) =>
            msg.id === assistantMsgId
              ? { ...msg, text: 'No response content received.', citations }
              : msg
          )
        );
      }
    } catch (err: any) {
      setMessages((prev) =>
        prev.map((msg) =>
          msg.id === assistantMsgId
            ? { ...msg, text: `Error: ${err.message}`, isStreaming: false }
            : msg
        )
      );
    } finally {
      setIsStreaming(false);
      setMessages((prev) =>
        prev.map((msg) =>
          msg.id === assistantMsgId ? { ...msg, isStreaming: false } : msg
        )
      );
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSendMessage();
    }
  };

  return (
    <div className="flex h-screen bg-[#18181b] text-zinc-100 font-sans antialiased">
      
      {/* Sidebar - Clean & Minimal */}
      <aside className="w-64 bg-[#121215] border-r border-zinc-800 flex flex-col p-3 flex-shrink-0">
        
        {/* New Chat Button */}
        <button
          onClick={() => setMessages([])}
          className="flex items-center justify-between w-full px-3 py-2 text-xs font-medium border border-zinc-700 rounded-lg hover:bg-zinc-800 transition"
        >
          <span>New Chat</span>
          <Plus className="w-4 h-4 text-zinc-400" />
        </button>

        {/* Documents Section */}
        <div className="flex-1 overflow-y-auto mt-4 space-y-1">
          <div className="text-[11px] font-semibold text-zinc-500 uppercase px-2 mb-2 tracking-wider">
            Documents ({docs.length})
          </div>

          {docs.length === 0 ? (
            <div className="text-xs text-zinc-500 px-2 py-2">No documents</div>
          ) : (
            docs.map((doc) => (
              <div
                key={doc.id}
                className="px-2.5 py-1.5 rounded bg-zinc-800/50 border border-zinc-800 flex items-center justify-between text-xs text-zinc-300"
              >
                <span className="truncate max-w-[140px]" title={doc.filename}>{doc.filename}</span>
                <span className="text-[10px] text-zinc-500 font-mono">{doc.num_chunks} chunks</span>
              </div>
            ))
          )}
        </div>

        {uploadStatus && (
          <div className="mt-2 text-xs text-zinc-400 p-2 bg-zinc-800 rounded border border-zinc-700 truncate">
            {uploadStatus}
          </div>
        )}
      </aside>

      {/* Main Container */}
      <main className="flex-1 flex flex-col h-full bg-[#18181b]">
        
        {/* Chat Messages */}
        <div className="flex-1 overflow-y-auto px-4 py-6">
          <div className="max-w-3xl mx-auto space-y-6">
            
            {messages.length === 0 ? (
              <div className="h-[70vh] flex flex-col items-center justify-center text-center">
                <h1 className="text-2xl font-semibold text-zinc-200 mb-2">RAG System</h1>
                <p className="text-sm text-zinc-400">Upload .txt, .md, or .pdf files and ask questions.</p>
              </div>
            ) : (
              messages.map((msg) => (
                <div
                  key={msg.id}
                  className={`flex flex-col ${msg.sender === 'user' ? 'items-end' : 'items-start'}`}
                >
                  {/* Sender Tag */}
                  <span className="text-[11px] text-zinc-500 mb-1 px-1 font-medium">
                    {msg.sender === 'user' ? 'You' : 'Assistant'}
                  </span>

                  {/* Message Bubble */}
                  <div
                    className={`max-w-2xl text-sm leading-relaxed px-4 py-3 rounded-xl ${
                      msg.sender === 'user'
                        ? 'bg-zinc-800 text-zinc-100'
                        : 'bg-zinc-900 border border-zinc-800 text-zinc-200 w-full'
                    }`}
                  >
                    {msg.sender === 'user' ? (
                      <div className="whitespace-pre-wrap">{msg.text}</div>
                    ) : (
                      <div>
                        {msg.citations && msg.citations.length > 0 && (
                          <div className="flex flex-wrap gap-1 mb-3">
                            {msg.citations.map((cit, i) => (
                              <span key={i} className="text-[10px] px-2 py-0.5 rounded bg-zinc-800 text-zinc-400 border border-zinc-700 font-mono">
                                {cit}
                              </span>
                            ))}
                          </div>
                        )}

                        <div className="markdown-body">
                          <ReactMarkdown>{msg.text || (msg.isStreaming ? 'Thinking...' : 'No response text')}</ReactMarkdown>
                        </div>
                      </div>
                    )}
                  </div>
                </div>
              ))
            )}

            <div ref={messagesEndRef} />
          </div>
        </div>

        {/* Input Bar */}
        <div className="p-4 bg-[#18181b]">
          <div className="max-w-3xl mx-auto">
            
            <form onSubmit={handleSendMessage} className="bg-zinc-900 border border-zinc-800 rounded-xl p-2.5 focus-within:border-zinc-600 transition">
              
              <input
                type="file"
                ref={fileInputRef}
                onChange={handleFileUpload}
                accept=".txt,.md,.pdf"
                className="hidden"
              />

              <textarea
                ref={textareaRef}
                value={inputPrompt}
                onChange={(e) => setInputPrompt(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="Ask a question..."
                rows={2}
                className="w-full bg-transparent text-sm text-zinc-100 placeholder-zinc-500 focus:outline-none resize-none px-2 pt-1"
              />

              <div className="flex items-center justify-between pt-2 border-t border-zinc-800/80 px-1 mt-1">
                
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={isIngesting}
                  className="p-1.5 rounded text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 transition flex items-center space-x-1 text-xs"
                >
                  <Paperclip className="w-4 h-4" />
                  <span>Upload file</span>
                </button>

                <button
                  type="submit"
                  disabled={!inputPrompt.trim() || isStreaming}
                  className={`p-1.5 rounded-lg transition ${
                    inputPrompt.trim() && !isStreaming
                      ? 'bg-zinc-100 text-zinc-900 hover:bg-white cursor-pointer'
                      : 'bg-zinc-800 text-zinc-600 cursor-not-allowed'
                  }`}
                >
                  <ArrowUp className="w-4 h-4" />
                </button>

              </div>

            </form>

          </div>
        </div>

      </main>

    </div>
  );
}
