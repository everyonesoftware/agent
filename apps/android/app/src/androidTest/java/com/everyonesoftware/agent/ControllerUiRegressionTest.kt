package com.everyonesoftware.agent

import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import kotlinx.coroutines.*
import okhttp3.mockwebserver.*
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import java.io.Closeable
import java.util.concurrent.CopyOnWriteArrayList

class ControllerUiRegressionTest {
 @get:Rule val compose=createAndroidComposeRule<ComponentActivity>()
 private inner class Fixture(val pending: Boolean=false, val resets: Boolean=false, val failConsume: Boolean=false) : Closeable {
  val requests=CopyOnWriteArrayList<RecordedRequest>()
  private val profile="profile_abcdefghijklmnopqrstuv"
  private val expiry=4102444800L
  private val status="""{"profile_id":"$profile","connection_id":"connection","state":"connected","available":true,"resets":{"available_count":1,"expirations":[{"count":1,"expires_at":$expiry}],"details_complete":true,"next":{"credit_id":"first-credit","expires_at":$expiry},"history":[{"kind":"expired","at":1795050000}]}}"""
  val server=MockWebServer()
  val controller=AgentController(MemorySessionStore(),CoroutineScope(SupervisorJob()+Dispatchers.Main.immediate))
  init {
   server.dispatcher=object: Dispatcher() { override fun dispatch(r: RecordedRequest): MockResponse {
    requests.add(r)
    val body=when(r.requestUrl!!.encodedPath) {
     "/api/auth/login", "/api/auth/me" -> "{\"id\":\"user\",\"username\":\"fixture\"}"
     "/api/conversations" -> "{\"conversations\":[]}"
     "/api/models" -> "{\"models\":[{\"id\":\"model\"}]}"
     "/api/auth/status" -> "{\"connected\":false}"
     "/api/auth/profiles" -> "{\"active_profile_id\":\"${if(pending) "" else "profile_abcdefghijklmnopqrstuv"}\",\"profiles\":[{\"id\":\"profile_abcdefghijklmnopqrstuv\",\"label\":\"Saved account\",\"registration_pending\":$pending}]}"
     "/api/usage/status" -> if(resets) status else """{"profile_id":"$profile","connection_id":"connection"}"""
     "/api/usage/resets/consume" -> if(failConsume) return MockResponse().setResponseCode(503).setBody("""{"error":"Could not verify the reset."}""") else
      """{"outcome":"reset","status":{"profile_id":"$profile","connection_id":"connection","state":"connected","available":true,"resets":{"available_count":0,"expirations":[],"details_complete":true,"history":[{"kind":"used","at":1796000000},{"kind":"expired","at":1795050000}]}}}"""
     "/api/research/settings" -> "{\"settings\":{\"revision\":\"current-revision\",\"configured\":true}}"
     "/api/research/settings/check" -> "{}"
     "/api/memories" -> "{\"entries\":[]}"
     "/auth/start" -> "{\"connected\":true,\"profile_id\":\"profile_abcdefghijklmnopqrstuv\"}"
     else -> return MockResponse().setResponseCode(404).setBody("{\"error\":\"Not found\"}")
    }
    return MockResponse().setBody(body).setHeader("Content-Type","application/json")
   } }
   val endpoint=server.url("/").toString()
   compose.runOnUiThread {
    compose.activity.setContent { AgentTheme { AgentApp(controller) } }
    controller.login(endpoint,"fixture","fixture-password",false)
   }
   compose.waitUntil(10000) { controller.state.value.user!=null && !controller.state.value.busy }
   compose.onNodeWithContentDescription("Settings").performClick()
   compose.waitUntil(10000) { controller.state.value.researchSettings!=null && !controller.state.value.busy }
  }
  fun click(label: String) {
   compose.onNodeWithTag("settings-scroll").performScrollToNode(hasText(label))
   compose.onNodeWithText(label).performClick()
   compose.waitUntil(10000) { !controller.state.value.busy }
  }
  override fun close() { compose.runOnUiThread { controller.close() }; server.close() }
 }
 @Test fun testSavedKeyButtonUsesActualCheckEndpoint() {
  Fixture().use { f ->
   f.click("Test saved key")
   val request=f.requests.last { it.method=="POST" }
   assertEquals("/api/research/settings/check",request.path)
   assertEquals(json("expected_revision" to "current-revision"),parseObject(request.body.clone().readUtf8()))
   assertEquals("",f.controller.state.value.error)
  }
 }
 @Test fun savedDisconnectedAccountHasReconnectSeparateFromAddAccount() {
  Fixture().use { f ->
   f.click("Reconnect with ChatGPT")
   assertEquals("/auth/start",f.requests.last { it.method=="POST" }.path)
   f.click("Add account")
   assertEquals("/auth/start?mode=add",f.requests.last { it.method=="POST" }.path)
  }
 }
 @Test fun pendingAccountSelectorResumesExactAccountWithoutPasswordConfirmation() {
  Fixture(pending=true).use { f ->
   compose.onNodeWithTag("settings-scroll").performScrollToNode(hasText("Select active account"))
   compose.onNodeWithText("Select active account").performClick()
   compose.onNodeWithText("Saved account").performClick()
   compose.waitUntil(10000) { f.requests.any { it.path?.startsWith("/auth/start?")==true } && !f.controller.state.value.busy }
   assertEquals("/auth/start?mode=retry&profile_id=profile_abcdefghijklmnopqrstuv",f.requests.last { it.method=="POST" }.path)
   compose.onNodeWithText("Agent password").assertDoesNotExist()
   f.click("Add account")
   assertEquals("/auth/start?mode=add",f.requests.last { it.method=="POST" }.path)
  }
 }
 @Test fun resetConfirmationCancelsWithoutWriteAndPublishesOnlyServerHistory() {
  Fixture(resets=true).use { f ->
   f.click("Use a reset")
   compose.onNodeWithText("Use 1 reset?").assertIsDisplayed()
   compose.onNode(hasText("Saved account",substring=true) and hasText("This can’t be undone.",substring=true)).assertExists()
   compose.onNodeWithText("This can’t be undone.",substring=true).assertExists()
   compose.onNodeWithText("Cancel").performClick()
   assertFalse(f.requests.any { it.method=="POST" })
   f.click("Use a reset")
   compose.onNodeWithText("Use reset").performClick()
   compose.waitUntil(10000) { !f.controller.state.value.busy }
   val request=f.requests.single { it.method=="POST" }
   assertEquals("/api/usage/resets/consume",request.path)
   val body=parseObject(request.body.clone().readUtf8())
   java.util.UUID.fromString(body.text("attempt_id"))
   assertEquals(json("profile_id" to "profile_abcdefghijklmnopqrstuv","connection_id" to "connection","credit_id" to "first-credit","attempt_id" to body.text("attempt_id"),
    "confirmed_count" to 1,"confirmed_expires_at" to 4102444800L),body)
   compose.onNodeWithTag("settings-scroll").performScrollToNode(hasText("Use a reset"))
   compose.onNodeWithText("Use a reset").assertIsNotEnabled()
   f.click("Reset history")
   compose.onNodeWithTag("settings-scroll").performScrollToNode(hasText("1 reset used"))
   compose.onAllNodesWithText("1 reset used").assertCountEquals(1)
  }
 }
 @Test fun uncertainResetDoesNotReplayOrKeepTheObsoleteOffer() {
  Fixture(resets=true,failConsume=true).use { f ->
   f.click("Use a reset")
   compose.onNodeWithText("Use reset").performClick()
   compose.waitUntil(10000) { !f.controller.state.value.busy }
   compose.onNodeWithTag("settings-scroll").performScrollToNode(hasText("Use a reset"))
   compose.onNodeWithText("Use a reset").assertIsNotEnabled()
   compose.onNodeWithText("Could not verify the reset.",substring=true).assertExists()
   assertEquals(1,f.requests.count { it.method=="POST" })
  }
 }
}
