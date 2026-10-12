// Call the local OpenAI-compatible API. Node.js 18+ (global fetch).
const base = process.env.YGG_BASE_URL || "http://127.0.0.1:7331";
const model = process.env.YGG_MODEL || "profile:general-assistant";
const apiKey = process.env.YGG_API_KEY || "";

function headers() {
  const h = { "Content-Type": "application/json" };
  if (apiKey) {
    h.Authorization = `Bearer ${apiKey}`;
  }
  return h;
}

const models = await fetch(`${base}/v1/models`, { headers: headers() });
if (!models.ok) {
  throw new Error(`GET /v1/models failed: ${models.status}`);
}
console.log(await models.json());


const chat = await fetch(`${base}/v1/chat/completions`, {
  method: "POST",
  headers: headers(),
  body: JSON.stringify({
    model,
    messages: [{ role: "user", content: "Hello" }],
    stream: true,
  }),
});

if (!chat.ok) {
  throw new Error(`POST /v1/chat/completions failed: ${chat.status}`);
}

if (!chat.body) {
  throw new Error("Chat completion response has no body");
}

const decoder = new TextDecoder();
let buffer = "";
let done = false;

function processLine(line) {
  const trimmed = line.trim();

  if (!trimmed.startsWith("data:")) return;

  const data = trimmed.slice(5).trim();

  if (data === "[DONE]") {
    done = true;
    return;
  }

  if (data) {
    console.log(JSON.parse(data));
  }
}

for await (const chunk of chat.body) {
  buffer += decoder.decode(chunk, { stream: true });

  const lines = buffer.split("\n");
  buffer = lines.pop() ?? "";

  for (const line of lines) {
    processLine(line);

    if (done) break;
  }

  if (done) break;
}

buffer += decoder.decode();

if (!done && buffer) {
  processLine(buffer);
}

if (!done) {
  throw new Error("Chat completion stream ended before data: [DONE]");
}
