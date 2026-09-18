import { app } from "../../scripts/app.js"
import { api } from "../../scripts/api.js"

const params = new URLSearchParams(window.location.search)
const enabled = params.get("h3_autoload") === "1"
const workflowPath = params.get("h3_workflow")
const sessionId = params.get("h3_session")
const callbackURL = params.get("h3_callback")

async function report(status, details = {}) {
  if (!callbackURL || !sessionId) return
  try {
    await fetch(callbackURL, {
      method: "POST",
      mode: "cors",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ session_id: sessionId, status, workflow_path: workflowPath, ...details })
    })
  } catch (error) {
    console.error("[H3 OneClick] readback callback failed", error)
  }
}

app.registerExtension({
  name: "H3OneClick.TemplateOpener",
  async setup() {
    if (!enabled || !workflowPath || !sessionId) return
    try {
      const response = await api.getUserData(workflowPath)
      if (!response.ok) throw new Error(`workflow read failed: ${response.status}`)
      const workflow = await response.json()
      const serialized = JSON.stringify(workflow)
      const hasH3 = serialized.includes("MiniMaxH3") || serialized.toLowerCase().includes("minimax_h3")
      await app.loadGraphData(workflow, true, true)
      await report("TEMPLATE_READY", { has_h3: hasH3, node_count: workflow.nodes?.length ?? 0 })
    } catch (error) {
      console.error("[H3 OneClick] workflow autoload failed", error)
      await report("TEMPLATE_BLOCKED", { message: String(error) })
    }
  }
})
