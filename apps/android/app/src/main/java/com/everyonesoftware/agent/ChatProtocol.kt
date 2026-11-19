package com.everyonesoftware.agent

import kotlinx.coroutines.CancellationException
import kotlinx.serialization.json.*
import java.util.UUID

fun chatRequest(history: List<JsonObject>, content: String, model: String, conversation: String): JsonObject {
 require(content.isNotBlank() && content.toByteArray().size <= 8192) { "Message must be 1–8192 UTF-8 bytes" }
 val messages = history.takeLast(39).map { json("role" to it.text("role"), "content" to it.text("content")) } + json("role" to "user", "content" to content)
 require(messages.all { it.text("role") in setOf("user", "assistant") && it.text("content").toByteArray().size <= 8192 }) { "History contains an oversized message; start a new chat" }
 val request = json("messages" to JsonArray(messages), "model" to model, "conversation_id" to conversation, "run_id" to UUID.randomUUID().toString(), "omitted_messages" to (history.size - 39).coerceAtLeast(0))
 require(request.toString().toByteArray().size <= 65536) { "Conversation exceeds 64 KiB; start a new chat" }
 return request
}
class ResearchFlow(private val api: AgentApi, val request: JsonObject) {
 var task: JsonObject? = null; private set
 suspend fun prepare(): JsonObject {
  check(task == null) { "Revoke the previous permission before preparing another task" }
  return api.request("POST", "/api/research/tasks", request).obj("task").also { result ->
   require(result.text("id").isNotEmpty() && result.text("scope") == "public_web" && result.text("status") == "pending") { "Invalid research permission" }
   task = result
  }
 }
 suspend fun approveAndSend(stillOwned: ()->Boolean = { true }): JsonObject {
  val id = requireNotNull(task).text("id")
  val approved = api.request("POST", "/api/research/tasks/$id/approve", json()).obj("task")
  check(approved.text("status") == "approved") { "Permission was not approved; no model request sent" }
  if(!stillOwned()) throw CancellationException("Research request is no longer active")
  return api.request("POST", "/api/chat", JsonObject(request + ("research_task_id" to JsonPrimitive(id))))
 }
 suspend fun end(action: String): JsonObject {
  require(action in setOf("stop", "revoke"))
  val id = requireNotNull(task).text("id")
  val result = api.request("POST", "/api/research/tasks/$id/$action", json()).obj("task")
  check(result.text("status") in setOf("revoked", "cancelled", "completed", "failed", "expired")) { "Server did not confirm cancellation; research may still be running" }
  return result
 }
}
