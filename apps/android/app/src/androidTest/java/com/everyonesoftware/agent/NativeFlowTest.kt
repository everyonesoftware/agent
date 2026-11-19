package com.everyonesoftware.agent

import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import okhttp3.mockwebserver.*
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test

class NativeFlowTest {
 @Test fun productionRouterChatAndSessionRestore() {
  val args=androidx.test.platform.app.InstrumentationRegistry.getArguments()
  val endpoint=requireNotNull(args.getString("agentFixture")) { "Pass -e agentFixture with the opt-in real-router fixture URL" }
  val store=SecureSessionStore(compose.activity)
  store.write(null)
  val controller=AgentController(store,CoroutineScope(SupervisorJob()+Dispatchers.Main.immediate))
  compose.runOnUiThread { compose.activity.setContent { AgentTheme { AgentApp(controller) } } }
  compose.onNodeWithTag("server").performTextInput(endpoint)
  compose.onNodeWithTag("username").performTextInput("android-${System.currentTimeMillis()}")
  compose.onNodeWithTag("password").performTextInput("native-fixture-password")
  compose.onNodeWithText("Create an Agent account").performClick()
  compose.onNodeWithTag("login").performClick()
  compose.waitUntil(30000) { compose.onAllNodesWithTag("composer").fetchSemanticsNodes().isNotEmpty() }
  compose.onNodeWithTag("composer").performTextInput("Native Android real-router acceptance")
  compose.onNodeWithTag("send").performClick()
  compose.waitUntil(30000) { compose.onAllNodesWithText("Native acceptance fixture reply. This response traversed the real Agent server and local test provider.").fetchSemanticsNodes().isNotEmpty() }
  assertNotNull(store.read())
  controller.close()
  val restored=AgentController(store,CoroutineScope(SupervisorJob()+Dispatchers.Main.immediate))
  compose.runOnUiThread { compose.activity.setContent { AgentTheme { AgentApp(restored) } } }
  compose.waitUntil(30000) { compose.onAllNodesWithTag("composer").fetchSemanticsNodes().isNotEmpty() }
  compose.onNodeWithContentDescription("Conversations").performClick()
  compose.onNodeWithText("Native Android real-router acceptance").assertExists()
  restored.close()
 }
 @get:Rule val compose = createAndroidComposeRule<ComponentActivity>()
 @Test fun loginChatHistoryAndApprovedSettingsReachRealTransport() {
  MockWebServer().use { server ->
   server.dispatcher = object: Dispatcher() { override fun dispatch(r: RecordedRequest): MockResponse {
    val body = when(r.requestUrl!!.encodedPath) {
     "/api/auth/login", "/api/auth/me" -> "{\"id\":\"u\",\"username\":\"fixture\"}"
     "/api/conversations" -> "{\"conversations\":[{\"id\":\"c\",\"title\":\"Saved chat\"}]}"
     "/api/conversations/c" -> "{\"messages\":[{\"role\":\"user\",\"content\":\"saved question\"},{\"role\":\"assistant\",\"content\":\"saved answer\",\"run_id\":\"run\"}]}"
     "/api/models" -> "{\"models\":[{\"id\":\"fixture-model\",\"name\":\"Fixture Model\"}]}"
     "/api/chat" -> "{\"reply\":\"Fixture reply\",\"conversation_id\":\"c\",\"run_id\":\"run\"}"
     "/api/auth/status" -> "{\"connected\":false,\"state\":\"disconnected\"}"
     "/api/auth/profiles" -> "{\"active_profile_id\":\"\",\"profiles\":[]}"
     "/api/research/settings" -> "{\"settings\":{\"revision\":\"r\",\"configured\":false,\"status\":\"not_configured\",\"budget\":{\"enabled\":false,\"maximum\":100,\"period\":\"month\",\"timezone\":\"UTC\",\"revision\":\"b\"}}}"
     "/api/memories" -> "{\"enabled\":true,\"entries\":[]}"
     else -> "{}"
    }
    return MockResponse().setHeader("Content-Type","application/json").setBody(body).apply { if(r.path == "/api/auth/login") setHeader("Set-Cookie","everyoneagent_session=fixture; Path=/; HttpOnly") }
   } }
   val controller = AgentController(MemorySessionStore(), CoroutineScope(SupervisorJob()+Dispatchers.Main.immediate))
   compose.runOnUiThread { compose.activity.setContent { AgentTheme { AgentApp(controller) } } }
   compose.onNodeWithTag("server").performTextInput(server.url("/").toString())
   compose.onNodeWithTag("username").performTextInput("fixture")
   compose.onNodeWithTag("password").performTextInput("fixture-not-real-password")
   compose.onNodeWithTag("login").performClick()
   compose.waitUntil(20000) { compose.onAllNodesWithTag("composer").fetchSemanticsNodes().isNotEmpty() }
   compose.onNodeWithTag("composer").performTextInput("hello")
   compose.onNodeWithTag("send").performClick()
   compose.waitUntil(20000) { compose.onAllNodesWithText("Fixture reply").fetchSemanticsNodes().isNotEmpty() }
   compose.onNodeWithContentDescription("Conversations").performClick()
   compose.onNodeWithText("Saved chat").performClick()
   compose.waitUntil(20000) { compose.onAllNodesWithText("saved answer").fetchSemanticsNodes().isNotEmpty() }
   compose.onNodeWithContentDescription("Settings").performClick()
   listOf("Updates","Connection","ChatGPT Connection","Web Research","Memory","Reset Password","Sign Out").forEach { section -> compose.onNodeWithTag("settings-scroll").performScrollToNode(hasText(section)); compose.onNodeWithText(section).assertExists() }
   assertTrue(server.requestCount >= 9)
   controller.close()
  }
 }
}
