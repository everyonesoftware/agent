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
  private const val profile="profile_abcdefghijklmnopqrstuv"
  private const val pendingAttempt="12345678-1234-4234-8234-123456789abc"
  private fun resetStatus(credit: String="first", count: Int=2, complete: Boolean=true) = """{
   "profile_id":"$profile","connection_id":"connection","state":"connected","available":true,
   "snapshot":{"stale":false,"windows":[{"id":"primary","used_percent":25}]},
   "resets":{"available_count":$count,"details_complete":$complete,
   "expirations":[{"count":1,"expires_at":null},{"count":1,"expires_at":4102444800}],
   "next":{"credit_id":"$credit","expires_at":4102444800},"history":[{"kind":"expired","at":1795050000},{"kind":"used","at":1796000000}]}}
  """.trimIndent()
  private fun AgentController.offer(): ResetOffer? = state.value.let { resetOffer(it,it.resetCredits(java.time.Instant.now().epochSecond)) }
 }
 @Test fun resetSummarySeparatesIncompleteMissingAndZeroAndUsesEasternTime() = runBlocking {
  Fixture(this).use { f ->
   f.handler={ r -> if(r.requestUrl!!.encodedPath=="/api/usage/status") response(resetStatus(count=3,complete=false)) else null }
   f.login(); f.settings()
   val s=f.controller.state.value
   val summary=s.resetCredits(1796000000L)!!
   assertEquals(3L,summary.count)
   assertEquals(listOf(4102444800L,null),summary.expirations.map { it.at })
   assertEquals(listOf("used","expired"),summary.history.map { it.kind })
   assertFalse(summary.complete)
   assertNull(f.controller.offer())
   assertNull(s.resetCredits(4102444800L))
   val usage=s.usage!!
   assertNull(s.copy(usage=JsonObject(usage-"resets")).resetCredits(1796000000L))
   assertNull(s.copy(usage=JsonObject(usage+("snapshot" to json("stale" to true)))).resetCredits(1796000000L))
   val zero=s.copy(usage=JsonObject(usage+("resets" to parseObject("""{"available_count":0,"details_complete":true,"expirations":[],"history":[]}"""))))
   assertEquals(0L,zero.resetCredits(1796000000L)?.count)
   assertEquals("Feb 1, 2026 at 7:00 AM ET",resetTime(java.time.Instant.parse("2026-02-01T12:00:00Z").epochSecond))
   assertEquals("Jul 1, 2026 at 8:00 AM ET",resetTime(java.time.Instant.parse("2026-07-01T12:00:00Z").epochSecond))
  }
 }
 @Test fun resetUsesExactConfirmationAndPublishesAuthoritativeStatusOnlyOnce() = runBlocking {
  Fixture(this).use { f ->
   val release=CountDownLatch(1)
   try {
    f.handler={ r -> when(r.requestUrl!!.encodedPath) {
     "/api/usage/status" -> response(resetStatus())
     "/api/usage/resets/consume" -> {
      release.await(5,TimeUnit.SECONDS)
      response("""{"outcome":"reset","status":${resetStatus(credit="different").replace("\"used_percent\":25","\"used_percent\":4")}}""")
     }
     else -> null
    } }
    f.login(); f.settings()
    val offer=f.controller.offer()!!
    assertEquals(2L,offer.count)
    assertEquals(4102444800L,offer.credit.expiresAt)
    val job=f.controller.consumeReset(offer)!!
    until { f.requests.any { it.path=="/api/usage/resets/consume" } }
    assertNull(f.controller.consumeReset(offer))
    release.countDown(); job.join()
    val request=f.requests.single { it.path=="/api/usage/resets/consume" }
    val body=parseObject(request.body.clone().readUtf8())
    java.util.UUID.fromString(body.text("attempt_id"))
    assertEquals(json("profile_id" to profile,"connection_id" to "connection","credit_id" to "first","attempt_id" to body.text("attempt_id"),
     "confirmed_count" to 2,"confirmed_expires_at" to 4102444800L),body)
    assertEquals("different",f.controller.offer()!!.credit.id)
    assertEquals(4.0,f.controller.state.value.usage!!.obj("snapshot").rows("windows").single().number("used_percent")!!,0.0)
    f.controller.consumeReset(offer)!!.join()
    assertEquals(1,f.requests.count { it.path=="/api/usage/resets/consume" })
   } finally { release.countDown() }
  }
 }
 @Test fun ambiguousResetWaitsForFreshReadAndExplicitConfirmationOfDurableAttempt() = runBlocking {
  Fixture(this).use { f ->
   val attempted=CopyOnWriteArrayList<String>()
   f.handler={ r -> when(r.requestUrl!!.encodedPath) {
    "/api/usage/status", "/api/usage/refresh" -> response(if(attempted.isEmpty()) resetStatus() else
     resetStatus().replace("\"credit_id\":\"first\"", "\"credit_id\":\"first\",\"pending_attempt_id\":\"${attempted.first()}\""))
    "/api/usage/resets/consume" -> {
     attempted.add(parseObject(r.body.clone().readUtf8()).text("attempt_id"))
     response("""{"error":"Could not verify the reset."}""",503)
    }
    else -> null
   } }
   f.login(); f.settings()
   val offer=f.controller.offer()!!
   f.controller.consumeReset(offer)!!.join()
   assertNull(f.controller.offer())
   assertTrue(f.controller.state.value.usage!!.obj("snapshot").flag("stale"))
   assertTrue(f.controller.state.value.resetFeedback.contains("Refresh allowance"))
   f.controller.refreshAllowance()!!.join()
   assertNotNull(f.controller.offer())
   f.controller.consumeReset(offer)!!.join()
   assertEquals("Readback and stale confirmation must not write",1,attempted.size)
   f.controller.consumeReset(f.controller.offer()!!)!!.join()
   assertEquals(listOf(attempted.first(),attempted.first()),attempted.toList())
  }
 }
 @Test fun freshControllerResumesServerAttemptWithExplicitNullExpiration() = runBlocking {
  Fixture(this).use { f ->
   val noExpiry="""{"available_count":1,"details_complete":true,"expirations":[{"count":1,"expires_at":null}],"next":{"credit_id":"first","expires_at":null,"pending_attempt_id":"$pendingAttempt"},"history":[]}"""
   val status=JsonObject(parseObject(resetStatus())+("resets" to parseObject(noExpiry)))
   f.handler={ r -> when(r.requestUrl!!.encodedPath) {
    "/api/usage/status" -> response(status.toString())
    "/api/usage/resets/consume" -> response("""{"outcome":"nothingToReset","status":$status}""")
    else -> null
   } }
   f.login(); f.settings()
   assertFalse(f.requests.any { it.path=="/api/usage/resets/consume" })
   f.controller.consumeReset(f.controller.offer()!!)!!.join()
   val body=parseObject(f.requests.single { it.path=="/api/usage/resets/consume" }.body.clone().readUtf8())
   assertEquals(json("profile_id" to profile,"connection_id" to "connection","credit_id" to "first","attempt_id" to pendingAttempt,
    "confirmed_count" to 1,"confirmed_expires_at" to null),body)
  }
 }
 @Test fun pendingResetCannotBeConfirmedAfterConnectionChanges() = runBlocking {
  Fixture(this).use { f ->
   val old=resetStatus().replace("\"credit_id\":\"first\"", "\"credit_id\":\"first\",\"pending_attempt_id\":\"$pendingAttempt\"")
   f.handler={ r -> if(r.requestUrl!!.encodedPath=="/api/usage/status") response(old) else null }
   f.login(); f.settings()
   val offer=f.controller.offer()!!
   val new=resetStatus().replace("\"connection_id\":\"connection\"", "\"connection_id\":\"replacement\"")
   f.handler={ r -> when(r.requestUrl!!.encodedPath) {
    "/api/usage/status" -> response(new)
    "/api/usage/resets/consume" -> response("""{"outcome":"nothingToReset","status":$new}""")
    else -> null
   } }
   f.controller.refreshSettings(); f.idle()
   f.controller.consumeReset(offer)!!.join()
   assertFalse(f.requests.any { it.path=="/api/usage/resets/consume" })
   f.controller.consumeReset(f.controller.offer()!!)!!.join()
   val body=parseObject(f.requests.single { it.path=="/api/usage/resets/consume" }.body.clone().readUtf8())
   assertEquals("replacement",body.text("connection_id"))
   assertNotEquals(pendingAttempt,body.text("attempt_id"))
  }
 }
 @Test fun malformedPendingAttemptLeavesBalanceVisibleButCannotBeConfirmed() = runBlocking {
  Fixture(this).use { f ->
   val bad=resetStatus().replace("\"credit_id\":\"first\"", "\"credit_id\":\"first\",\"pending_attempt_id\":\"not-an-attempt\"")
   f.handler={ r -> if(r.requestUrl!!.encodedPath=="/api/usage/status") response(bad) else null }
   f.login(); f.settings()
   assertEquals(2L,f.controller.state.value.resetCredits(1796000000L)?.count)
   assertNull(f.controller.offer())
  }
 }
 @Test fun definitiveNoCreditLetsFreshEligibleStatusOfferSameCreditAgain() = runBlocking {
  Fixture(this).use { f ->
   val attempted=CopyOnWriteArrayList<String>()
   f.handler={ r -> when(r.requestUrl!!.encodedPath) {
    "/api/usage/status", "/api/usage/refresh" -> response(resetStatus())
    "/api/usage/resets/consume" -> {
     attempted.add(parseObject(r.body.clone().readUtf8()).text("attempt_id"))
     val result=if(attempted.size==1) parseObject(resetStatus()).let { JsonObject(it-"resets") }.toString() else resetStatus()
     response("""{"outcome":"noCredit","status":$result}""")
    }
    else -> null
   } }
   f.login(); f.settings()
   f.controller.consumeReset(f.controller.offer()!!)!!.join()
   assertNull(f.controller.offer())
   f.controller.refreshAllowance()!!.join()
   assertNotNull(f.controller.offer())
   assertEquals(1,attempted.size)
   f.controller.consumeReset(f.controller.offer()!!)!!.join()
   assertEquals(2,attempted.distinct().size)
   assertNotNull(f.controller.offer())
  }
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
