# FUNCTION_INVOCATION_FAILED: Fix and Mental Model

This doc explains **why** the error happened, **how** we fixed it, and **how to avoid** it in the future.

---

## 1. The fix (what we changed)

### A. Removed the legacy serverless handler

- **Deleted:** `api/index.js` and `api/set-public-dir.js`
- **Reason:** The app is Next.js with **App Router** API routes (`app/api/**/route.ts`). The `api/` folder at the project root is the **Vercel legacy** serverless convention. Having both meant:
  - Next.js registered handlers for `/api/transcribe`, `/api/recordings`, etc.
  - Vercel could also treat `api/index.js` as a function for `/api`.
  - If the legacy function was invoked, it tried to load Express from `../server.js` and run it. That code path was built for the old setup (different bundling, env, and no longer maintained), so it could **crash** (uncaught exception or module error) → **FUNCTION_INVOCATION_FAILED**.

By removing the legacy handler, only Next.js route handlers run for `/api/*`, so there’s no conflicting or broken code path.

### B. Hardened API route handlers

- **Dynamic routes** (`[id]`, `[filename]`): Safely read `params` and wrapped the handler body in `try/catch` so that:
  - Missing or wrong-shaped `params` (e.g. in some Next/Node versions) don’t cause **uncaught** errors.
  - Any `fs` or `JSON.parse` failure returns a **500 JSON response** instead of crashing the process.
- **Other routes** (`transcribe-preview`, `transcribe`): Ensured **all** async work (e.g. `request.formData()`) is inside a `try` block so that **every** thrown error is caught and turned into a response (400/500) instead of an unhandled rejection.

Rule of thumb: **every serverless route handler must eventually return a Response (or throw in a way that the framework turns into one). No unhandled rejections or uncaught exceptions.**

---

## 2. Root cause (why it happened)

### What the code was doing vs what it needed to do

- **What it was doing:**  
  - Next.js correctly defined API routes in `app/api/...`.  
  - **But** the old Vercel-style `api/index.js` was still present and tried to run the previous Express app when invoked. That path was no longer valid (wrong env, missing or different bundle, Express not set up for this build), so when that function ran it could throw (e.g. at import or first request) and **never return a response**.

- **What it needed to do:**  
  - Only the **Next.js** handlers should run for `/api/*`.  
  - **Every** code path in those handlers must either return a `NextResponse` or throw in a way that’s handled (so the runtime still gets a response). No process crash and no unhandled rejection.

### What conditions triggered this error

- **Vercel** runs your code in a **serverless function**. When that function:
  - throws an **uncaught exception**, or
  - has an **unhandled promise rejection**, or
  - **exits/crashes** before sending a response,  
  Vercel has nothing to send to the client, so it returns **500** and labels it **FUNCTION_INVOCATION_FAILED**.

So the trigger is: **something in the function threw or crashed and was never caught, so the function didn’t complete with a response.**

### What misconception or oversight led to this

- **Leftover convention:** Keeping the old `api/` entrypoint from the pre–Next.js setup, without realizing Vercel might still invoke it and that it was incompatible with the current build.
- **Assuming “only Next.js runs”:** It’s easy to assume that once you use Next.js, only `app/api` runs. In reality, both the **Next.js build** and the **legacy `api/`** layout can coexist in one repo; Vercel may still deploy and invoke the legacy one unless it’s removed.
- **Narrow try/catch:** Some routes only wrapped the “main” logic in try/catch. Early steps (e.g. `await request.formData()` or reading `params`) could throw and weren’t covered, so one thrown error could take down the whole invocation.

---

## 3. Underlying concept: why this error exists

### Why does this error exist and what is it protecting?

- **Serverless contract:** A serverless function is invoked to **handle one request**. The platform expects the function to **return a response** (or signal success/failure in a defined way). If the process crashes or an error is unhandled, the platform has no response to send, so it must return a generic **500** and mark the invocation as failed.
- **Protection:** It’s telling you: “This invocation did not complete successfully; the function did not return a valid response.” That protects the client from hanging and makes it clear that the failure is on the server side, so you look at **logs and function code**, not at the client.

### Correct mental model

- **Every request → one function run → one response.**  
  Your handler should guarantee that, for every request path that hits it, it **always** ends by:
  - returning a `NextResponse` (or equivalent), or
  - throwing in a way that your framework/runtime converts into a response (e.g. error boundary or global handler).
- **No “half-finished” requests.**  
  If you throw and don’t catch, the runtime has no response. So: **catch at the boundary** (top of the handler or a wrapper) and turn errors into 4xx/5xx responses.

### How this fits into framework/language design

- **Node/JavaScript:** Uncaught exceptions and unhandled rejections can terminate the process or leave the process in a bad state. In long-running servers you might have a process-level handler; in **serverless**, each invocation is short-lived, so the **function** must handle errors and return a response.
- **Next.js / Vercel:** Route handlers are async functions. If they throw or reject and that’s not caught, the framework has nothing to send. So the framework doesn’t “fix” it for you; **you** must ensure every path returns (or is converted to) a response.

---

## 4. Warning signs and similar mistakes

### What to look for

- **Legacy and new API side by side:** If you ever had a non-Next serverless API (e.g. `api/index.js`, `api/*.js`), and you later add Next.js `app/api/`, remove or rename the old entrypoints so only one “owner” handles `/api`.
- **Unprotected top-level or early steps:**  
  - `await request.formData()`, `await request.json()`, or reading `params` can throw.  
  - If only the “core” logic is in try/catch, one of these can cause FUNCTION_INVOCATION_FAILED.
- **Sync code that can throw:** `fs.readFileSync`, `JSON.parse`, `path.join` with bad input, or destructuring `undefined` (e.g. `const { id } = await params` when `params` is wrong). All of these should be inside a try/catch that returns a 4xx/5xx.

### Similar mistakes in related scenarios

- **Other serverless platforms** (AWS Lambda, Cloudflare Workers, etc.): Same idea — the handler must **always** return a response (or use a documented error mechanism). Uncaught errors → invocation failed.
- **Edge vs Node:** In Edge runtimes you don’t have Node `fs` or some Node APIs. Using them can throw at runtime and cause the same kind of failure if not caught.
- **Cold start + imports:** If a **top-level** import or initializer throws (e.g. missing env, wrong module), the function can fail before it even runs your handler. Fix by making sure env and dependencies are correct and that you don’t do risky work at module scope.

### Code smells

- Route handler with no try/catch and multiple `await` or sync calls that can throw.
- Mixed API styles in one repo (e.g. both `api/index.js` and `app/api/`) without a clear plan for which handles what.
- Relying on “it works locally” without thinking: “What if `params` is undefined? What if `formData()` rejects?”

---

## 5. Alternatives and trade-offs

### A. One top-level try/catch per handler (what we did)

- **Approach:** Wrap the entire handler body in a single try/catch; on catch, return `NextResponse.json({ error: ... }, { status: 500 })`.
- **Pros:** Simple, guarantees a response for any thrown error in that handler.  
- **Cons:** All errors become 500 unless you add more specific catches; you may want to log or report errors before returning.

### B. Route-level error boundary / wrapper

- **Approach:** A HOF or wrapper that runs every route and catches errors, e.g. `withErrorHandling(handler)`.
- **Pros:** Single place to log and format errors; handlers stay focused on “happy path.”  
- **Cons:** Next.js doesn’t provide this out of the box for App Router route handlers; you’d add it yourself or via middleware (which has different semantics).

### C. Removing legacy and relying on framework

- **Approach:** Only use Next.js API routes; remove legacy `api/` so there’s no second, brittle code path.
- **Pros:** One clear contract, no conflicting handlers.  
- **Cons:** You must ensure all routes are correctly implemented and that nothing else is still pointing at the old handler.

### D. Global unhandled rejection handler

- **Approach:** In Node, `process.on('unhandledRejection', ...)`.
- **Pros:** Can log or report.  
- **Cons:** In serverless, you still need to **return a response** from the request that caused the rejection; the unhandled-rejection handler doesn’t have access to the request/response. So it doesn’t replace per-handler try/catch for guaranteeing a response.

**Recommendation:** Use **A** (per-handler try/catch) plus **C** (remove legacy handler). Add **B** only if you want a single place to log/format errors across many routes.

---

## Quick checklist for future API routes

- [ ] No legacy `api/*.js` (or similar) still bound to the same paths as Next.js.
- [ ] Entire handler body in try/catch (or equivalent) so **every** error path returns a response.
- [ ] `params` (and similar) validated before use (e.g. `params?.id` / `typeof rawId === 'string'`).
- [ ] Any sync calls that can throw (`fs`, `JSON.parse`, destructuring) inside try/catch.
- [ ] Env and dependencies needed at cold start are correct so module load doesn’t throw.
