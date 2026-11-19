package com.everyonesoftware.agent

import kotlinx.coroutines.*
import kotlinx.serialization.json.*
import okhttp3.mockwebserver.*
import org.junit.Assert.*
import org.junit.Test
import java.io.Closeable
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class AgentControllerTest {
 private class Fixture(owner: CoroutineScope) : Closeable {
  val server=MockWebServer()
  val requests=CopyOnWriteArrayList<RecordedRequest>()
  var handler: (RecordedRequest)->MockResponse? = { null }
  val scope=CoroutineScope(owner.coroutineContext+SupervisorJob())
  val controller: AgentController
  init {
   server.dispatcher=object: Dispatcher() { override fun dispatch(request: RecordedRequest): MockResponse {
    requests.add(request)
    return handler(request) ?: response(when(request.requestUrl!!.encodedPath) {
     "/api/auth/login", "/api/auth/me" -> "{\"id\":\"user\",\"username\":\"fixture\"}"
     "/api/conversations" -> "{\"conversations\":[]}"
     "/api/models" -> "{\"models\":[{\"id\":\"model\"}]}"
     "/api/auth/status" -> "{\"connected\":false}"
     "/api/auth/profiles" -> "{\"active_profile_id\":\"profile_abcdefghijklmnopqrstuv\",\"profiles\":[{\"id\":\"profile_abcdefghijklmnopqrstuv\"}]}"
     "/api/usage/status" -> "{\"profile_id\":\"profile_abcdefghijklmnopqrstuv\",\"connection_id\":\"connection\",\"snapshot\":{\"stale\":false,\"fetched_at\":123,\"windows\":[{\"id\":\"primary\",\"used_percent\":25}]}}"
     "/api/research/settings" -> "{\"settings\":{\"revision\":\"revision\",\"configured\":true}}"
     "/api/memories" -> "{\"entries\":[]}"
     else -> "{}"
    })
   } }
   controller=AgentController(MemorySessionStore(),scope)
  }
  suspend fun login() { controller.login(server.url("/").toString(),"fixture","fixture-password",false); idle() }
  suspend fun idle() { until { !controller.state.value.busy } }
  suspend fun settings() { controller.navigate("settings"); idle() }
  override fun close() { controller.close(); server.close() }
 }
 companion object {
  private fun response(body: String, code: Int=200)=MockResponse().setResponseCode(code).setBody(body).setHeader("Content-Type","application/json")
  private suspend fun until(check: ()->Boolean) { withTimeout(10000) { while(!check()) delay(5) } }
 }
 @Test fun switchingProviderAccountInvalidatesPreparedResearchOwnership() = runBlocking {
  Fixture(this).use { f ->
   f.login(); f.settings()
   f.handler={ r -> if(r.path=="/api/research/tasks") response("{\"task\":{\"id\":\"task\",\"scope\":\"public_web\",\"status\":\"pending\"}}") else null }
   f.controller.send("research",true); f.idle()
   assertNotNull(f.controller.state.value.pendingResearch)
   f.controller.changeProfile("profile_otheraccountidentifier","fixture-password",false); f.idle()
   assertNull("Pending permission belongs to the old provider account",f.controller.state.value.pendingResearch)
   f.controller.approveResearch()?.join()
   assertFalse(f.requests.any { it.path=="/api/research/tasks/task/approve" })
  }
 }
 @Test fun stopDuringApprovalDoesNotStartChatAfterRevocation() = runBlocking {
  Fixture(this).use { f ->
   val approval=CountDownLatch(1)
   try {
    f.login()
    f.handler={ r -> when(r.path) {
     "/api/research/tasks" -> response("{\"task\":{\"id\":\"task\",\"scope\":\"public_web\",\"status\":\"pending\"}}")
     "/api/research/tasks/task/approve" -> { approval.await(5,TimeUnit.SECONDS); response("{\"task\":{\"status\":\"approved\"}}") }
     "/api/research/tasks/task/stop" -> response("{\"task\":{\"status\":\"cancelled\"}}")
     "/api/chat" -> response("{\"reply\":\"must not be requested\"}")
     else -> null
    } }
    f.controller.send("research",true); f.idle()
    val owner=f.controller.approveResearch()!!
    until { f.requests.any { it.path=="/api/research/tasks/task/approve" } }
    f.controller.endResearch("stop")!!.join()
    approval.countDown(); owner.join(); f.idle()
    assertFalse("A revoked request must not proceed to chat",f.requests.any { it.path=="/api/chat" })
    assertTrue(f.controller.state.value.messages.isEmpty())
   } finally { approval.countDown() }
  }
 }
 @Test fun testSavedKeyUsesCheckRouteWithRevisionAndNoCredential() = runBlocking {
  Fixture(this).use { f ->
   f.login(); f.settings()
   f.handler={ r -> if(r.requestUrl!!.encodedPath.startsWith("/api/research/settings/")) {
    if(r.path=="/api/research/settings/check") response("{}") else response("{\"error\":\"Not found\"}",404)
   } else null }
   f.controller.testSavedResearchKey(); f.idle()
   val request=f.requests.last { it.method=="POST" }
   assertEquals("/api/research/settings/check",request.path)
   assertEquals(json("expected_revision" to "revision"),parseObject(request.body.readUtf8()))
   assertEquals("",f.controller.state.value.error)
  }
 }
 @Test fun visibleConnectionActionsReconnectAndResumeExactPendingAccountSeparatelyFromAdd() = runBlocking {
  for(pending in listOf(false,true)) Fixture(this).use { f ->
   val profile="profile_abcdefghijklmnopqrstuv"
   f.handler={ r -> when(r.requestUrl!!.encodedPath) {
    "/api/auth/profiles" -> response("{\"active_profile_id\":\"${if(pending) "" else profile}\",\"profiles\":[{\"id\":\"$profile\",\"label\":\"Saved account\",\"registration_pending\":$pending}]}")
    "/auth/start" -> response("{\"connected\":true,\"profile_id\":\"$profile\"}")
    else -> null
   } }
   f.login(); f.settings()
   val options=deviceAuthorizations(f.controller.state.value)
   assertEquals("Reconnect/resume must be separate from Add account",2,options.size)
   val resume=options.single { it.mode!="add" }
   assertEquals(if(pending) "retry" else "",resume.mode)
   assertEquals(if(pending) profile else "",resume.profile)
   f.controller.startDevice(resume.mode,resume.profile); f.idle()
   val start=f.requests.last { it.requestUrl!!.encodedPath=="/auth/start" }
   assertEquals(if(pending) "/auth/start?mode=retry&profile_id=$profile" else "/auth/start",start.path)
   assertEquals(if(pending) "" else profile,parseObject(start.body.readUtf8()).text("expected_active_profile_id"))
   val add=options.single { it.mode=="add" }
   f.controller.startDevice(add.mode,add.profile); f.idle()
   assertEquals("/auth/start?mode=add",f.requests.last { it.requestUrl!!.encodedPath=="/auth/start" }.path)
  }
 }
 @Test fun stopResearchKeepsOwnerGateUntilLateChatSettlesForCancelledAndCompletedRaces() = runBlocking {
  for(terminal in listOf("cancelled","completed")) Fixture(this).use { f ->
   val release=CountDownLatch(1)
   try {
    f.login()
    f.handler={ r -> when(r.path) {
     "/api/research/tasks" -> response("{\"task\":{\"id\":\"task\",\"scope\":\"public_web\",\"status\":\"pending\"}}")
     "/api/research/tasks/task/approve" -> response("{\"task\":{\"status\":\"approved\"}}")
     "/api/research/tasks/task/stop" -> response("{\"task\":{\"status\":\"$terminal\"}}")
     "/api/chat" -> { release.await(5,TimeUnit.SECONDS); response("{\"reply\":\"late research\",\"conversation_id\":\"old\"}") }
     else -> null
    } }
    f.controller.send("research question",true); f.idle()
    val owner=f.controller.approveResearch()!!
    until { f.requests.any { it.path=="/api/chat" } }
    val stop=f.controller.endResearch("stop")!!; stop.join()
    assertTrue("Stop must not release the original chat mutation gate ($terminal)",f.controller.state.value.busy)
    assertNull(f.controller.send("new question",false))
    f.controller.newChat()
    assertNull(f.controller.openConversation(json("id" to "new")))
    release.countDown(); owner.join(); f.idle()
    assertTrue("Stopped request must not append a late reply",f.controller.state.value.messages.isEmpty())
    f.controller.newChat()
    assertEquals("",f.controller.state.value.conversation)
   } finally { release.countDown() }
  }
 }
 @Test fun settingsNavigationWinsOverDelayedConversationAndRunAndStillRefreshes() = runBlocking {
  for(path in listOf("/api/conversations/old","/api/runs/old")) Fixture(this).use { f ->
   val release=CountDownLatch(1)
   try {
    f.login()
    f.handler={ r -> if(r.path==path) { release.await(5,TimeUnit.SECONDS); response(if(path.contains("conversations")) "{\"messages\":[{\"content\":\"old\"}]}" else "{\"id\":\"old\"}") } else null }
    if(path.contains("conversations")) f.controller.openConversation(json("id" to "old","title" to "Old")) else f.controller.inspectRun("old")
    until { f.requests.any { it.path==path } }
    f.controller.navigate("settings")
    release.countDown(); f.idle()
    assertEquals("settings",f.controller.state.value.screen)
    assertEquals("",f.controller.state.value.conversation)
    assertNull(f.controller.state.value.run)
    assertNotNull("Settings refresh must not be silently discarded",f.controller.state.value.profiles)
   } finally { release.countDown() }
  }
 }
 @Test fun failedAllowanceRefreshKeepsEvidenceButMarksLastKnown() = runBlocking {
  Fixture(this).use { f ->
   f.login(); f.settings()
   val before=f.controller.state.value.usage!!.obj("snapshot")
   f.handler={ r -> when(r.requestUrl!!.encodedPath) {
    "/api/usage/refresh" -> response("{\"error\":\"Refresh failed\"}",503)
    "/api/usage/status" -> response("{\"profile_id\":\"profile_abcdefghijklmnopqrstuv\",\"connection_id\":\"connection\"}")
    else -> null
   } }
   f.controller.refreshAllowance(); f.idle()
   val usage=f.controller.state.value.usage!!
   assertTrue(usage.obj("snapshot").flag("stale"))
   assertEquals(before["windows"],usage.obj("snapshot")["windows"])
   assertEquals(before["fetched_at"],usage.obj("snapshot")["fetched_at"])
   assertEquals("Refresh failed",usage.text("error"))
  }
 }
 @Test fun settingsRefreshRetriesFailedModelDiscovery() = runBlocking {
  Fixture(this).use { f ->
   var fail=true
   f.handler={ r -> when {
    r.path=="/api/models" && fail -> response("{\"error\":\"temporarily unavailable\"}",503)
    r.path=="/api/conversations" && !fail -> response("{\"error\":\"conversation list unavailable\"}",503)
    else -> null
   } }
   f.login(); assertTrue(f.controller.state.value.model.isEmpty())
   fail=false; f.settings()
   assertEquals("model",f.controller.state.value.model)
  }
 }
 @Test fun protectedSwitchAndDisconnectSendOnlyPasswordAndExactProfile() = runBlocking {
  Fixture(this).use { f ->
   f.login(); f.settings()
   for(disconnect in listOf(false,true)) {
    f.controller.changeProfile("profile_abcdefghijklmnopqrstuv","correct-horse-battery",disconnect); f.idle()
    val path=if(disconnect) "/api/auth/disconnect" else "/api/auth/profiles/active"
    val payload=parseObject(f.requests.last { it.requestUrl!!.encodedPath==path }.body.readUtf8())
    assertEquals(setOf("profile_id","password"),payload.keys)
    assertEquals("profile_abcdefghijklmnopqrstuv",payload.text("profile_id"))
    assertEquals("correct-horse-battery",payload.text("password"))
   }
  }
 }
}
