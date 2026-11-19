package com.everyonesoftware.agent

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class ChatProtocolTest {
 @Test fun chatBoundsContextAndSendsNoToolsOrConsentForOrdinaryChat() {
  val old = (0..49).map { json("role" to if(it%2==0) "user" else "assistant", "content" to "message $it", "run_id" to "ignored") }
  val request = chatRequest(old, "hello", "real-discovered-model", "conversation")
  assertEquals(40, request.rows("messages").size)
  assertEquals(11, request.number("omitted_messages")!!.toInt())
  assertEquals("hello", request.rows("messages").last().text("content"))
  assertFalse(request.containsKey("tools")); assertFalse(request.containsKey("research_task_id"))
  assertEquals(setOf("role", "content"), request.rows("messages").first().keys)
  assertThrows(IllegalArgumentException::class.java) { chatRequest(emptyList(), "x".repeat(8193), "m", "") }
 }
 @Test fun researchRequiresSeparateApprovalAndReusesExactRequest() = runBlocking {
  MockWebServer().use { server ->
   server.enqueue(MockResponse().setBody("{\"task\":{\"id\":\"t\",\"status\":\"pending\",\"scope\":\"public_web\"}}"))
   server.enqueue(MockResponse().setBody("{\"task\":{\"id\":\"t\",\"status\":\"approved\"}}"))
   server.enqueue(MockResponse().setBody("{\"reply\":\"actual response\",\"conversation_id\":\"c\"}"))
   val api = AgentApi(server.url("/").toString(), MemorySessionStore())
   val flow = ResearchFlow(api, chatRequest(emptyList(), "research this", "m", ""))
   flow.prepare()
   assertEquals(1, server.requestCount)
   assertEquals("/api/research/tasks", server.takeRequest().path)
   val reply = flow.approveAndSend()
   assertEquals("actual response", reply.text("reply"))
   assertEquals("/api/research/tasks/t/approve", server.takeRequest().path)
   val sent = parseObject(server.takeRequest().body.readUtf8())
   assertEquals("t", sent.text("research_task_id"))
   assertEquals(flow.request, JsonObject(sent.filterKeys { it != "research_task_id" }))
  }
 }
}
