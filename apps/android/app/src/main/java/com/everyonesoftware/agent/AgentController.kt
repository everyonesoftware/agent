package com.everyonesoftware.agent

import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.serialization.json.*
import java.net.URLEncoder

fun enc(value: String): String = URLEncoder.encode(value,"UTF-8")
data class AgentState(
 val endpoint: String = "", val user: JsonObject? = null, val restoring: Boolean = false,
 val busy: Boolean = false, val error: String = "", val notice: String = "", val screen: String = "chat",
 val conversations: List<JsonObject> = emptyList(), val conversation: String = "", val title: String = "New conversation", val messages: List<JsonObject> = emptyList(),
 val models: List<JsonObject> = emptyList(), val model: String = "", val status: JsonObject? = null, val profiles: JsonObject? = null, val usage: JsonObject? = null,
 val researchSettings: JsonObject? = null, val memory: JsonObject? = null, val deviceLogin: JsonObject? = null,
 val pendingResearch: JsonObject? = null, val activeResearch: String = "", val run: JsonObject? = null,
 val history: JsonObject? = null, val historyProvider: String = "chatgpt", val historyRange: String = "24h", val historyWindow: String = ""
)

data class DeviceAuthorization(val label: String, val mode: String = "", val profile: String = "")
fun deviceAuthorizations(state: AgentState): List<DeviceAuthorization> {
 if(state.profiles==null || state.status==null) return emptyList()
 val profiles=state.profiles.rows("profiles")
 val active=profiles.firstOrNull { it.text("id")==state.profiles.text("active_profile_id") }
 return buildList {
  if(state.status.flag("connected") && state.status.flag("plan_usage_enabled")) { /* Already ready. */ }
  else if(active?.flag("registration_pending")==true) add(DeviceAuthorization("Resume sign-in","retry",active.text("id")))
  else if(active!=null || profiles.none { it.flag("registration_pending") }) add(DeviceAuthorization(if(profiles.isEmpty()) "Connect ChatGPT" else "Reconnect with ChatGPT"))
  if(active==null) profiles.filter { it.flag("registration_pending") }.forEach { add(DeviceAuthorization("Resume sign-in · "+it.text("label").ifBlank { it.text("id") },"retry",it.text("id"))) }
  if(profiles.isNotEmpty()) add(DeviceAuthorization("Add account","add"))
 }
}

/** One account epoch owns every request; no old response can populate a new account. */
class AgentController(private val store: SessionStore, private val scope: CoroutineScope) {
 private val mutable = MutableStateFlow(AgentState())
 val state: StateFlow<AgentState> = mutable.asStateFlow()
 private var api: AgentApi? = null
 private var epoch = 0
 private var navigationGeneration = 0
 private var settingsRefreshPending = false
 private var activeJob: Job? = null
 private var researchControlJob: Job? = null
 private var pollJob: Job? = null
 private var research: ResearchFlow? = null
 private data class ChatOwner(val account: Int, val conversation: String, val request: String, val generation: Int)
 private var chatGeneration = 0
 private var researchOwner: ChatOwner? = null
 private var stoppedResearch: ResearchFlow? = null
 private fun owns(owner: ChatOwner, request: JsonObject) = owner.account==epoch && owner.conversation==state.value.conversation && owner.generation==chatGeneration && owner.request==request.text("run_id")
 private fun update(change: (AgentState)->AgentState) { mutable.update(change) }
 init { store.read()?.let { session -> api = AgentApi(session.endpoint,store); update { it.copy(endpoint=session.endpoint,restoring=true) }; action { val user = api!!.get("/api/auth/me"); update { it.copy(user=user,restoring=false) }; refreshChat() } } }
 private fun action(block: suspend ()->Unit): Job? {
  if(state.value.busy) return null
  val captured = epoch
  update { it.copy(busy=true,error="",notice="") }
  return scope.launch(start=CoroutineStart.LAZY) {
   try { block() } catch(c: CancellationException) { throw c } catch(e: Exception) {
    if(captured == epoch) {
     if(e is ApiFailure && e.status == 401 && e.data.text("action") != "reauthorize") {
      // Confirm an actual expired Agent cookie, not a rejected password/provider credential.
      val alive = try { api?.get("/api/auth/me") != null } catch (_: Exception) { false }
      if(!alive) { invalidate(); api?.forget(); update { AgentState(endpoint=it.endpoint,error="Session expired. Sign in again.") }; return@launch }
     }
     update { it.copy(error=e.message ?: "Request failed",restoring=false) }
    }
   } finally { if(captured == epoch) {
    // Stop is a separate control request, never the mutation owner. Wait for
    // both requests before permitting a newer conversation/account mutation.
    researchControlJob?.join()
    if(captured==epoch) {
    update { it.copy(busy=false) }
    if(settingsRefreshPending && state.value.screen=="settings") refreshSettings()
    }
   } }
  }.also { activeJob = it; it.start() }
 }
 private fun invalidate() { epoch++; navigationGeneration++; chatGeneration++; settingsRefreshPending=false; activeJob?.cancel(); researchControlJob?.cancel(); researchControlJob=null; pollJob?.cancel(); api?.cancel(); research=null; researchOwner=null; stoppedResearch=null }
 fun close() { invalidate(); scope.cancel() }
 fun login(endpoint: String, username: String, password: String, register: Boolean) {
  if(state.value.busy) return
  try {
   val origin = validatedEndpoint(endpoint); invalidate(); api = AgentApi(origin,store)
   update { AgentState(endpoint=origin) }
   action { val user = api!!.login(username,password,register); update { it.copy(user=user) }; refreshChat() }
  } catch(e: Exception) { update { it.copy(error=e.message ?: "Invalid server address") } }
 }
 private suspend fun refreshChat() {
  val client = requireNotNull(api)
  val conversations = client.get("/api/conversations").rows("conversations")
  update { it.copy(conversations=conversations) }
  discoverModels()
 }
 private suspend fun discoverModels() {
  val client=requireNotNull(api)
  try { val models=client.get("/api/models").rows("models"); update { it.copy(models=models,model=it.model.takeIf { m -> models.any { row -> row.text("id")==m } } ?: models.firstOrNull()?.text("id").orEmpty()) } }
  catch(e: Exception) { if(e is CancellationException) throw e; update { it.copy(models=emptyList(),model="",notice="Models unavailable. Open Settings to refresh the connection.") } }
 }
 fun refreshModels() = action { refreshChat() }
 fun chooseModel(value: String) { if(!state.value.busy) update { it.copy(model=value) } }
 fun newChat() { if(!state.value.busy) { navigationGeneration++; chatGeneration++; research=null; researchOwner=null; update { it.copy(conversation="",title="New conversation",messages=emptyList(),screen="chat",run=null,error="",pendingResearch=null) } } }
 fun openConversation(item: JsonObject) = action {
  val navigation=++navigationGeneration
  val id=item.text("id"); val messages=api!!.get("/api/conversations/${enc(id)}").rows("messages")
  if(navigation==navigationGeneration) { chatGeneration++; research=null; researchOwner=null; update { it.copy(conversation=id,title=item.text("title"),messages=messages,screen="chat",run=null,pendingResearch=null) } }
 }
 fun deleteConversation(item: JsonObject) = action {
  val id=item.text("id"); api!!.request("DELETE","/api/conversations/${enc(id)}")
  val rows=api!!.get("/api/conversations").rows("conversations")
  update { it.copy(conversations=rows,conversation=if(it.conversation==id) "" else it.conversation,messages=if(it.conversation==id) emptyList() else it.messages,title=if(it.conversation==id) "New conversation" else it.title) }
 }
 fun send(content: String, browse: Boolean) = action {
  check(state.value.model.isNotBlank()) { "Discover an available model first" }
  val snapshot=state.value
  val request=chatRequest(snapshot.messages,content.trim(),snapshot.model,snapshot.conversation)
  val owner=ChatOwner(epoch,snapshot.conversation,request.text("run_id"),++chatGeneration)
  if(browse) {
   val flow=ResearchFlow(api!!,request); research=flow; researchOwner=owner; stoppedResearch=null
   val task=flow.prepare(); if(owns(owner,request) && research===flow) update { it.copy(pendingResearch=task) }
  } else completeChat(request,api!!.request("POST","/api/chat",request),owner)
 }
 private suspend fun completeChat(request: JsonObject, result: JsonObject, owner: ChatOwner) {
  if(!owns(owner,request)) return
  check(result.text("reply").isNotBlank()) { "Server returned no reply" }
  update { it.copy(messages=it.messages+request.rows("messages").last()+json("role" to "assistant","content" to result.text("reply"),"run_id" to result.text("run_id")),conversation=result.text("conversation_id").ifBlank { it.conversation },title=if(it.messages.isEmpty()) request.rows("messages").last().text("content").take(80) else it.title,pendingResearch=null,activeResearch="") }
  // Acknowledged conversation identity is retained even if list readback fails.
  val rows=api!!.get("/api/conversations").rows("conversations"); if(owner.account==epoch && owner.generation==chatGeneration) update { it.copy(conversations=rows) }
 }
 fun approveResearch() = action {
  val flow=requireNotNull(research); val owner=requireNotNull(researchOwner)
  check(owns(owner,flow.request)) { "Research conversation changed; prepare a new task" }
  update { it.copy(pendingResearch=null,activeResearch=flow.task!!.text("id")) }
  try { val result=flow.approveAndSend { owns(owner,flow.request) && research===flow && stoppedResearch!==flow }; if(research===flow && stoppedResearch!==flow) completeChat(flow.request,result,owner) }
  finally { if(owner.account==epoch && owner.generation==chatGeneration) update { it.copy(activeResearch="") } }
 }
 fun endResearch(action: String = "revoke"): Job? {
  val flow=research ?: return null
  val owner=researchOwner ?: return null
  if(!owns(owner,flow.request) || researchControlJob?.isActive==true) return null
  val stop: suspend ()->Unit = {
   stoppedResearch=flow
   flow.end(action)
   if(owns(owner,flow.request) && research===flow) {
    update { it.copy(pendingResearch=null,activeResearch="",notice=if(action=="stop") "Research stopped" else "Research permission revoked") }
    research=null; researchOwner=null
   }
  }
  if(!state.value.busy) return action(block=stop)
  // Only a currently running research request permits this parallel control.
  if(state.value.activeResearch!=flow.task?.text("id")) return null
  return scope.launch(start=CoroutineStart.LAZY) {
   try { stop() } catch(c: CancellationException) { throw c } catch(e: Exception) {
    if(owns(owner,flow.request)) update { it.copy(error=e.message ?: "Could not stop research; it may still be running") }
   }
  }.also { researchControlJob=it; it.start() }
 }
 fun answerWithoutBrowsing() = action {
  val flow=requireNotNull(research); val owner=requireNotNull(researchOwner)
  check(owns(owner,flow.request)) { "Research conversation changed; prepare a new task" }
  flow.end("revoke"); update { it.copy(pendingResearch=null) }; research=null; researchOwner=null
  completeChat(flow.request,api!!.request("POST","/api/chat",flow.request),owner)
 }
 fun inspectRun(id: String) = action { val navigation=++navigationGeneration; val run=api!!.get("/api/runs/${enc(id)}"); if(navigation==navigationGeneration) update { it.copy(run=run,screen="run") } }
 fun navigate(screen: String) { navigationGeneration++; update { it.copy(screen=screen) }; if(screen=="settings") refreshSettings() }
 fun refreshSettings(): Job? {
  if(state.value.busy) { settingsRefreshPending=true; return null }
  settingsRefreshPending=false
  return action { loadSettings(); discoverModels() }
 }
 private suspend fun safeRead(path: String): JsonObject? = try { api!!.get(path) } catch(e: Exception) { if(e is CancellationException) throw e; update { it.copy(notice="Some settings could not be loaded. Refresh to retry.") }; null }
 private suspend fun loadSettings() {
  update { it.copy(status=null,profiles=null,usage=null,researchSettings=null,memory=null) }
  val status=safeRead("/api/auth/status"); val profiles=safeRead("/api/auth/profiles")
  update { it.copy(status=status,profiles=profiles) }
  val profile=profiles?.text("active_profile_id").orEmpty()
  if(profile.isNotBlank()) { val usage=safeRead("/api/usage/status?profile_id=${enc(profile)}"); update { it.copy(usage=usage) } }
  val settings=safeRead("/api/research/settings")?.obj("settings"); val memory=safeRead("/api/memories")
  update { it.copy(researchSettings=settings,memory=memory) }
 }
 fun refreshAllowance() = action {
  val s=state.value; val profile=s.profiles?.text("active_profile_id").orEmpty(); val connection=s.usage?.text("connection_id").orEmpty()
  check(profile.isNotEmpty() && connection.isNotEmpty()) { "Refresh the ChatGPT connection first" }
  try {
   val usage=api!!.request("POST","/api/usage/refresh",json("profile_id" to profile,"connection_id" to connection))
   check(usage.text("profile_id")==profile) { "Allowance account changed. Refresh Settings to retry." }
   update { it.copy(usage=usage) }
  } catch(e: Exception) {
   if(e is CancellationException) throw e
   fun lastKnown(usage: JsonObject): JsonObject = JsonObject(usage + ("error" to JsonPrimitive(e.message ?: "Allowance refresh failed")) +
    (if(usage["snapshot"] is JsonObject) mapOf("snapshot" to JsonObject(usage.obj("snapshot") + ("stale" to JsonPrimitive(true)))) else emptyMap()))
   update { it.copy(usage=it.usage?.let(::lastKnown)) }
   // Recover mutation preconditions without replaying an ambiguous refresh.
   val current=try { api!!.get("/api/usage/status?profile_id=${enc(profile)}") } catch(c: CancellationException) { throw c } catch(_: Exception) { null }
   if(current?.text("profile_id")==profile) {
    val evidence=if(current["snapshot"] is JsonObject || current.text("connection_id")!=connection || s.usage?.get("snapshot") !is JsonObject) current
     else JsonObject(current + ("snapshot" to s.usage.obj("snapshot")))
    update { it.copy(usage=lastKnown(evidence)) }
   }
   throw e
  }
 }
 fun startDevice(mode: String = "", profile: String = "") = action {
  check(state.value.profiles != null) { "Refresh Settings first" }
  val path="/auth/start"+(if(mode.isNotEmpty()) "?mode=${enc(mode)}"+(if(profile.isNotEmpty()) "&profile_id=${enc(profile)}" else "") else "")
  val login=api!!.request("POST",path,json("expected_active_profile_id" to state.value.profiles!!.text("active_profile_id")))
  if(login.flag("connected")) loadSettings() else {
   validatedVerificationUrl(login.text("verification_url")); check(login.text("profile_id").isNotEmpty() && login.text("connection_id").isNotEmpty() && login.text("login_id").isNotEmpty())
   update { it.copy(deviceLogin=login) }; pollDevice(login)
  }
 }
 private fun pollDevice(login: JsonObject) {
  pollJob?.cancel(); val captured=epoch
  pollJob=scope.launch { while(captured==epoch && state.value.deviceLogin==login) {
   delay(3000)
   try {
    val status=api!!.get("/api/usage/status?profile_id=${enc(login.text("profile_id"))}&connection_id=${enc(login.text("connection_id"))}")
    if(captured != epoch) return@launch
    if(status.text("state") == "connected" && status["login"] !is JsonObject) { update { it.copy(deviceLogin=null) }; loadSettings(); refreshChat(); return@launch }
    if(System.currentTimeMillis()/1000 >= (login.number("expires_at") ?: 0.0)) { update { it.copy(deviceLogin=null,notice="ChatGPT sign-in expired; try again") }; return@launch }
   } catch(e: Exception) { if(e is CancellationException) throw e; update { it.copy(notice="Could not check sign-in. Retry or cancel; the existing account is kept.") } }
  } }
 }
 fun cancelDevice() = action {
  val login=requireNotNull(state.value.deviceLogin)
  api!!.request("POST","/api/usage/cancel",json("profile_id" to login.text("profile_id"),"connection_id" to login.text("connection_id"),"login_id" to login.text("login_id")))
  pollJob?.cancel(); update { it.copy(deviceLogin=null) }; loadSettings()
 }
 fun changeProfile(id: String, password: String, disconnect: Boolean) = action {
  api!!.request("POST",if(disconnect) "/api/auth/disconnect" else "/api/auth/profiles/active",json("profile_id" to id,"password" to password))
  // Invalidate all provider-specific snapshots before refreshing the newly selected account.
  chatGeneration++; research=null; researchOwner=null; stoppedResearch=null
  pollJob?.cancel(); update { it.copy(usage=null,history=null,run=null,status=null,profiles=null,deviceLogin=null,pendingResearch=null,activeResearch="") }
  loadSettings(); refreshChat()
  if(disconnect) update { it.copy(notice="Tokens cleared on this host. Remote revocation was not confirmed; review ChatGPT settings.") }
 }
 fun testSavedResearchKey() = researchSetting("check")
 fun researchSetting(action: String, key: String = "") = action {
  val current=requireNotNull(state.value.researchSettings)
  val payload=if(action=="verify") json("expected_revision" to current.text("revision"),"api_key" to key) else json("expected_revision" to current.text("revision"))
  api!!.request("POST","/api/research/settings/$action",payload)
  val fresh=api!!.get("/api/research/settings").obj("settings"); update { it.copy(researchSettings=fresh,notice="Research settings refreshed") }
 }
 fun budget(enabled: Boolean, maximum: String, period: String, timezone: String) = action {
  val current=requireNotNull(state.value.researchSettings)
  val max=requireNotNull(maximum.toIntOrNull()?.takeIf { it in 1..10000000 }) { "Choose a cap between 1 and 10000000" }
  api!!.request("POST","/api/research/settings/budget",json("expected_revision" to current.text("revision"),"expected_budget_revision" to current.obj("budget").text("revision"),"enabled" to enabled,"maximum" to max,"period" to period,"timezone" to timezone))
  val fresh=api!!.get("/api/research/settings").obj("settings"); update { it.copy(researchSettings=fresh) }
 }
 fun memoryEnabled(enabled: Boolean) = action { api!!.request("PUT","/api/memory-settings",json("enabled" to enabled)); val memory=api!!.get("/api/memories"); update { it.copy(memory=memory) } }
 fun saveMemory(id: String, category: String, content: String) = action {
  require(content.trim().toByteArray().size in 1..1024)
  api!!.request(if(id.isEmpty()) "POST" else "PUT","/api/memories"+(if(id.isEmpty()) "" else "/${enc(id)}"),json("category" to category,"content" to content))
  val memory=api!!.get("/api/memories"); update { it.copy(memory=memory,notice="Memory saved") }
 }
 fun deleteMemory(entry: JsonObject) = action { api!!.request("DELETE","/api/memories/${enc(entry.text("id"))}",headers=mapOf("If-Match" to JsonPrimitive(entry.text("updated_at")).toString())); val memory=api!!.get("/api/memories"); update { it.copy(memory=memory) } }
 fun password(current: String, next: String, confirm: String) = action { require(next==confirm && next.length>=12 && next.toByteArray().size<=72) { "New passwords must match, with at least 12 characters and at most 72 bytes" }; api!!.request("POST","/api/auth/password",json("current_password" to current,"new_password" to next)); api!!.get("/api/auth/me"); update { it.copy(notice="Password changed. Other Agent sessions were signed out.") } }
 fun signOut() = action { api!!.request("POST","/api/auth/logout"); invalidate(); api!!.forget(); update { AgentState(endpoint=it.endpoint) } }
 fun changeServer() { invalidate(); api?.forget(); api=null; update { AgentState() } }
 fun history(provider: String, range: String = "24h", window: String = "") = action {
  require(provider in setOf("chatgpt","brave") && range in setOf("24h","week","period"))
  update { it.copy(screen="history",history=null,historyProvider=provider,historyRange=range,historyWindow=window) }
  val query="range=$range"+(if(provider=="chatgpt") "&profile_id=${enc(state.value.profiles?.text("active_profile_id").orEmpty())}&window_id=${enc(window)}" else "")
  val data=api!!.get((if(provider=="chatgpt") "/api/usage/history?" else "/api/research/history?")+query)
  check(data.text("provider")==provider && data["points"] is JsonArray)
  update { it.copy(history=data,historyWindow=data.text("window_id")) }
 }
}
fun validatedVerificationUrl(value: String): String {
 val uri=java.net.URI(value)
 require(uri.scheme=="https" && uri.host in setOf("auth.openai.com","chatgpt.com") && uri.userInfo==null && (uri.port == -1 || uri.port==443)) { "Untrusted ChatGPT approval URL" }
 return value
}
