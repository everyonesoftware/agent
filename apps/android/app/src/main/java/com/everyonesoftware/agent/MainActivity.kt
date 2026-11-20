package com.everyonesoftware.agent

import android.os.Bundle
import android.content.Intent
import android.net.Uri
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter

class AgentViewModel(app: android.app.Application): androidx.lifecycle.AndroidViewModel(app) {
 val controller=AgentController(SecureSessionStore(app),viewModelScope)
 val updates=UpdateManager(app,viewModelScope)
 override fun onCleared() { controller.close() }
}
class MainActivity: ComponentActivity() {
 override fun onCreate(savedInstanceState: Bundle?) {
  super.onCreate(savedInstanceState)
  val model=ViewModelProvider(this)[AgentViewModel::class.java]
  setContent { AgentTheme { AgentApp(model.controller,model.updates) } }
 }
}
@Composable fun AgentTheme(content: @Composable ()->Unit) {
 MaterialTheme(colorScheme=darkColorScheme(primary=Color(0xffb4d6a3),onPrimary=Color(0xff1c281a),background=Color(0xff202322),surface=Color(0xff202322),surfaceContainer=Color(0xff252a27),onSurface=Color(0xffe1e6e2),outline=Color(0xff333936)),content=content)
}
@Composable private fun AgentBrand(style: TextStyle) {
 Row(verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(10.dp)) {
  Image(painterResource(R.drawable.agent_icon),contentDescription=null,modifier=Modifier.size(26.dp))
  Text("Agent",style=style)
 }
}
@Composable private fun Field(label: String,value: String,change: (String)->Unit,secret: Boolean=false,tag: String="",enabled: Boolean=true) {
 OutlinedTextField(value,change,label={ Text(label) },modifier=Modifier.fillMaxWidth().testTag(tag),enabled=enabled,visualTransformation=if(secret) PasswordVisualTransformation() else VisualTransformation.None,singleLine=true)
}
@Composable private fun Action(label: String,enabled: Boolean=true,click: ()->Unit) { OutlinedButton(onClick=click,enabled=enabled) { Text(label) } }
@Composable private fun Section(title: String,content: @Composable ColumnScope.()->Unit) {
 Column(Modifier.fillMaxWidth(),verticalArrangement=Arrangement.spacedBy(10.dp)) { Text(title,style=MaterialTheme.typography.titleLarge); Surface(color=Color(0xff252a27),shape=MaterialTheme.shapes.medium) { Column(Modifier.padding(16.dp).fillMaxWidth(),verticalArrangement=Arrangement.spacedBy(10.dp),content=content) } }
}
@Composable private fun Disclosure(title: String,onOpen: ()->Unit={},content: @Composable ()->Unit) {
 var open by remember { mutableStateOf(false) }
 TextButton(onClick={ open=!open; if(open) onOpen() }) { Text((if(open) "▾ " else "› ")+title) }
 if(open) content()
}
@Composable fun AgentApp(controller: AgentController,updates: UpdateManager?=null) {
 val state by controller.state.collectAsState()
 val drawer=rememberDrawerState(DrawerValue.Closed); val scope=rememberCoroutineScope()
 BackHandler(state.screen!="chat" && state.user!=null) { controller.navigate("chat") }
 MaterialTheme { Surface(Modifier.fillMaxSize()) {
 if(state.restoring) Box(Modifier.fillMaxSize(),contentAlignment=Alignment.Center) { Column(horizontalAlignment=Alignment.CenterHorizontally) { AgentBrand(style=MaterialTheme.typography.headlineMedium); CircularProgressIndicator(); Text("Loading Agent…") } }
 else if(state.user==null) Login(controller,state)
 else ModalNavigationDrawer(drawerState=drawer,drawerContent={ ModalDrawerSheet(drawerContainerColor=Color(0xff181b1a)) {
  Box(Modifier.padding(24.dp)) { AgentBrand(style=MaterialTheme.typography.headlineMedium) }
  NavigationDrawerItem(label={ Text("New conversation") },selected=false,onClick={ controller.newChat(); scope.launch { drawer.close() } })
  LazyColumn(Modifier.weight(1f)) { items(state.conversations,key={ it.text("id") }) { item -> NavigationDrawerItem(label={ Text(item.text("title").ifBlank { "Untitled conversation" }) },selected=state.conversation==item.text("id"),onClick={ controller.openConversation(item); scope.launch { drawer.close() } }) } }
  NavigationDrawerItem(label={ Text("Settings") },selected=state.screen=="settings",onClick={ controller.navigate("settings"); scope.launch { drawer.close() } })
 } }) {
 Scaffold(topBar={ Column { Row(Modifier.fillMaxWidth().padding(horizontal=8.dp),verticalAlignment=Alignment.CenterVertically) {
  IconButton(onClick={ if(state.screen=="chat") scope.launch { drawer.open() } else controller.navigate("chat") }) { Icon(if(state.screen=="chat") Icons.Default.Menu else Icons.AutoMirrored.Filled.ArrowBack,if(state.screen=="chat") "Conversations" else "Back to chat") }
  Text(if(state.screen=="chat") state.title else state.screen.replaceFirstChar { it.uppercase() },Modifier.weight(1f),style=MaterialTheme.typography.titleMedium,maxLines=2)
  IconButton(onClick={ controller.newChat() }) { Icon(Icons.Default.Add,"New conversation") }
  IconButton(onClick={ controller.navigate("settings") }) { Icon(Icons.Default.Settings,"Settings") }
 }; if(state.busy) LinearProgressIndicator(Modifier.fillMaxWidth()) } }) { padding -> Column(Modifier.padding(padding).fillMaxSize()) {
  if(state.error.isNotEmpty()) Text(state.error,color=MaterialTheme.colorScheme.error,modifier=Modifier.padding(12.dp))
  if(state.notice.isNotEmpty()) Text(state.notice,modifier=Modifier.padding(12.dp),style=MaterialTheme.typography.bodySmall)
  when(state.screen) {
   "settings" -> SettingsView(controller,state,updates)
   "history" -> HistoryView(controller,state)
   "run" -> SelectionContainer { Text(state.run?.toString().orEmpty(),Modifier.padding(16.dp).verticalScroll(rememberScrollState())) }
   else -> ChatView(controller,state)
  }
 } }
 }
 if(state.pendingResearch!=null) AlertDialog(onDismissRequest={},title={ Text("Allow public-web research?") },text={ Column { Text("This task may send searches to Brave Search. Permission applies only to this task; ordinary chat remains tool-free."); Text(state.pendingResearch!!.toString(),style=MaterialTheme.typography.bodySmall) } },confirmButton={ TextButton(onClick={ controller.approveResearch() },enabled=!state.busy) { Text("Allow this task") } },dismissButton={ Column { TextButton(onClick={ controller.answerWithoutBrowsing() },enabled=!state.busy) { Text("Answer without browsing") }; TextButton(onClick={ controller.endResearch() },enabled=!state.busy) { Text("Cancel") } } })
 } }
}
@Composable private fun Login(controller: AgentController,state: AgentState) {
 var endpoint by remember { mutableStateOf(state.endpoint) }; var username by remember { mutableStateOf("") }; var password by remember { mutableStateOf("") }; var register by remember { mutableStateOf(false) }
 Column(Modifier.fillMaxSize().imePadding().verticalScroll(rememberScrollState()).padding(24.dp),verticalArrangement=Arrangement.spacedBy(16.dp)) {
  Spacer(Modifier.height(40.dp)); AgentBrand(style=MaterialTheme.typography.headlineLarge); Text("Your private conversation workspace")
  Field("Server address",endpoint,{ endpoint=it },tag="server"); Field("Username",username,{ username=it },tag="username"); Field("Password",password,{ password=it },true,"password")
  if(state.error.isNotEmpty()) Text(state.error,color=MaterialTheme.colorScheme.error)
  Button(onClick={ controller.login(endpoint,username,password,register); password="" },enabled=!state.busy && endpoint.isNotBlank() && username.isNotBlank() && password.isNotEmpty(),modifier=Modifier.testTag("login")) { Text(if(register) "Create account" else "Sign in") }
  TextButton(onClick={ register=!register },enabled=!state.busy) { Text(if(register) "Already have an account? Sign in" else "Create an Agent account") }
 }
}
@Composable private fun ChatView(controller: AgentController,state: AgentState) {
 var draft by remember(state.conversation) { mutableStateOf("") }; var browse by remember { mutableStateOf(false) }; var models by remember { mutableStateOf(false) }
 Column(Modifier.fillMaxSize().imePadding()) {
  val list=androidx.compose.foundation.lazy.rememberLazyListState()
  LaunchedEffect(state.messages.size) { if(state.messages.isNotEmpty()) list.animateScrollToItem(state.messages.lastIndex) }
  LazyColumn(Modifier.weight(1f).fillMaxWidth(),state=list,contentPadding=PaddingValues(16.dp),verticalArrangement=Arrangement.spacedBy(16.dp)) {
   if(state.messages.isEmpty()) item { Column(Modifier.padding(top=48.dp),verticalArrangement=Arrangement.spacedBy(12.dp)) { Text("What’s on your mind?",style=MaterialTheme.typography.headlineMedium); Text("Start a conversation. Your history stays on your Agent server.") } }
   items(state.messages) { message -> Column(Modifier.fillMaxWidth().background(if(message.text("role")=="user") Color(0xff2b332d) else Color.Transparent,MaterialTheme.shapes.medium).padding(12.dp)) { Text(if(message.text("role")=="user") "You" else "Agent",color=MaterialTheme.colorScheme.primary,style=MaterialTheme.typography.labelMedium); SelectionContainer { Text(message.text("content")) }; if(message.text("run_id").isNotEmpty()) TextButton(onClick={ controller.inspectRun(message.text("run_id")) }) { Text("Inspect run") } } }
  }
  Column(Modifier.fillMaxWidth().background(Color(0xff181b1a)).padding(12.dp),verticalArrangement=Arrangement.spacedBy(6.dp)) {
   Box { TextButton(onClick={ models=true },enabled=!state.busy) { Text(state.models.firstOrNull { it.text("id")==state.model }?.text("name")?.ifBlank { state.model } ?: "Select model") }; DropdownMenu(expanded=models,onDismissRequest={ models=false }) { state.models.forEach { model -> DropdownMenuItem(text={ Text(model.text("name").ifBlank { model.text("id") }) },onClick={ controller.chooseModel(model.text("id")); models=false }) } } }
   Row(verticalAlignment=Alignment.CenterVertically) { Checkbox(browse,{ browse=it },enabled=!state.busy); Text("Request web research",style=MaterialTheme.typography.bodySmall) }
   OutlinedTextField(draft,{ draft=it },placeholder={ Text("Message Agent…") },modifier=Modifier.fillMaxWidth().heightIn(max=180.dp).testTag("composer"),maxLines=5,enabled=!state.busy)
   Row(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.End) { if(state.activeResearch.isNotEmpty()) Action("Stop research") { controller.endResearch("stop") }; Button(onClick={ controller.send(draft,browse); draft="" },enabled=!state.busy && draft.isNotBlank() && state.model.isNotBlank(),modifier=Modifier.testTag("send")) { Text("Send") } }
  }
 }
}
@Composable private fun SettingsView(controller: AgentController,state: AgentState,updates: UpdateManager?) {
 LazyColumn(Modifier.fillMaxSize().testTag("settings-scroll"),contentPadding=PaddingValues(16.dp),verticalArrangement=Arrangement.spacedBy(24.dp)) {
  item { Section("Updates") { UpdatesView(updates) } }
  item { Section("Connection") { Text("Home server",style=MaterialTheme.typography.titleMedium); Text(state.endpoint); Text(state.user?.text("username").orEmpty()); Action("Refresh connection",!state.busy) { controller.refreshSettings() }; Action("Change server",!state.busy) { controller.changeServer() } } }
  item { Section("ChatGPT Connection") { ChatGPTView(controller,state) } }
  item { Section("Web Research") { ResearchSettings(controller,state) } }
  item { Section("Memory") { MemoryView(controller,state) } }
  item { Section("Reset Password") { PasswordView(controller,state) } }
  item { Section("Sign Out") { var confirm by remember { mutableStateOf(false) }; Text("Sign out on this device. Saved conversations stay on your server."); Action("Sign out",!state.busy) { confirm=true }; if(confirm) AlertDialog(onDismissRequest={ confirm=false },title={ Text("Sign out of Agent?") },confirmButton={ TextButton(onClick={ confirm=false; controller.signOut() }) { Text("Confirm sign out") } },dismissButton={ TextButton(onClick={ confirm=false }) { Text("Cancel") } }) } }
 }
}
@Composable private fun UpdatesView(manager: UpdateManager?) {
 val context=LocalContext.current
 Text("Current Version: ${BuildConfig.VERSION_NAME}",style=MaterialTheme.typography.titleMedium)
 if(manager==null) { Text("Not checked yet"); return }
 val state by manager.state.collectAsState()
 val working=state.phase in setOf("Checking…","Downloading…","Verifying…")
 Action("Check for updates",!working && state.phase!="Ready to install") { manager.check() }
 if(state.offer==null) Text(state.phase) else {
  Row(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.SpaceBetween) { Text("Update available",Modifier.weight(1f),style=MaterialTheme.typography.titleMedium); Text("Agent ${state.offer!!.version}",Modifier.weight(1f),color=MaterialTheme.colorScheme.primary) }
  Disclosure("What’s new"+(state.notes?.let { " · ${it.count { change -> change.visible }} changes" } ?: ""),{ manager.loadNotes() }) {
   if(state.notes==null) { Text(state.notesStatus.ifBlank { "Changelog unavailable" }); if(state.notesStatus!="Loading…") Action("Retry changelog") { manager.loadNotes() } }
   else { val notes=state.notes!!; listOf(false,true).forEach { fixes -> val rows=notes.filter { it.visible && it.fix==fixes }; if(rows.isNotEmpty()) { Text(if(fixes) "Fixes" else "Improvements",color=MaterialTheme.colorScheme.primary); rows.forEach { Text("• ${it.bullet}") } } }; Disclosure("Commit details") { SelectionContainer { Column { Text("${BuildConfig.SOURCE_COMMIT}…${state.offer!!.commit}"); notes.forEach { Text("${it.sha.take(7)} ${it.subject}",style=MaterialTheme.typography.bodySmall) } } } } }
  }
  if(working) { Text(state.phase); LinearProgressIndicator(progress={ state.progress },modifier=Modifier.fillMaxWidth()); if(state.phase=="Downloading…") Action("Cancel download") { manager.cancelDownload() } }
  else if(state.phase=="Ready to install") Button(onClick={ manager.install { context.startActivity(it) } }) { Text("Install update") }
  else Button(onClick={ manager.download() }) { Text("Download update") }
 }
 if(state.error.isNotEmpty()) Text(state.error,color=MaterialTheme.colorScheme.error)
}
private fun timeText(seconds: Double?): String = if(seconds==null || seconds<=0) "Unknown" else DateTimeFormatter.ofPattern("MMM d, HH:mm").withZone(ZoneId.systemDefault()).format(Instant.ofEpochSecond(seconds.toLong()))
@Composable private fun ChatGPTView(controller: AgentController,state: AgentState) {
 val active=state.profiles?.text("active_profile_id").orEmpty(); val profiles=state.profiles?.rows("profiles").orEmpty()
 var select by remember { mutableStateOf(false) }; var protectedId by remember { mutableStateOf<String?>(null) }; var disconnect by remember { mutableStateOf(false) }; var password by remember { mutableStateOf("") }
 Row(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.SpaceBetween) { Text(state.status?.text("state")?.ifBlank { if(state.status?.flag("connected")==true) "Connected" else "Not connected" } ?: "Status unavailable"); IconButton(onClick={ controller.refreshSettings() },enabled=!state.busy) { Icon(Icons.Default.Refresh,"Refresh ChatGPT status") } }
 Box { OutlinedButton(onClick={ select=true },enabled=!state.busy && profiles.isNotEmpty()) { Text(profiles.firstOrNull { it.text("id")==active }?.let { it.text("label").ifBlank { it.text("email").ifBlank { it.text("id") } } } ?: "Select active account") }; DropdownMenu(select,{ select=false }) { profiles.forEach { profile -> DropdownMenuItem(text={ Text(profile.text("label").ifBlank { profile.text("email").ifBlank { profile.text("id") } }) },onClick={ select=false; if(profile.flag("registration_pending")) controller.startDevice("retry",profile.text("id")) else if(profile.text("id")!=active) { protectedId=profile.text("id"); disconnect=false } }) } } }
 deviceAuthorizations(state).forEach { authorization -> Action(authorization.label,!state.busy && state.profiles!=null && state.deviceLogin==null) { controller.startDevice(authorization.mode,authorization.profile) } }
 if(active.isNotEmpty()) Action("Disconnect",!state.busy) { protectedId=active; disconnect=true }
 state.deviceLogin?.let { login -> Text("Enter this code on the OpenAI verification page:"); SelectionContainer { Text(login.text("user_code"),style=MaterialTheme.typography.headlineMedium) }; val context=LocalContext.current; Action("Open verification page") { context.startActivity(Intent(Intent.ACTION_VIEW,Uri.parse(validatedVerificationUrl(login.text("verification_url"))))) }; Action("Cancel sign-in",!state.busy) { controller.cancelDevice() } }
 if(protectedId!=null) AlertDialog(onDismissRequest={ protectedId=null; password="" },title={ Text(if(disconnect) "Disconnect ChatGPT" else "Switch active account") },text={ Column { Text("Enter your Agent password to confirm."); Field("Agent password",password,{ password=it },true) } },confirmButton={ TextButton(onClick={ controller.changeProfile(protectedId!!,password,disconnect); protectedId=null; password="" },enabled=password.isNotEmpty() && !state.busy) { Text("Confirm") } },dismissButton={ TextButton(onClick={ protectedId=null; password="" }) { Text("Cancel") } })
 if(active.isNotEmpty()) {
  Row(Modifier.fillMaxWidth(),horizontalArrangement=Arrangement.SpaceBetween,verticalAlignment=Alignment.CenterVertically) { Text("Allowance",style=MaterialTheme.typography.titleMedium); IconButton(onClick={ controller.refreshAllowance() },enabled=!state.busy) { Icon(Icons.Default.Refresh,"Refresh allowance") } }
  val snapshot=state.usage?.obj("snapshot"); val windows=snapshot?.rows("windows").orEmpty()
  Text(if(windows.isEmpty()) "Allowance unavailable" else (if(snapshot!!.flag("stale")) "Last known · " else "Updated · ")+timeText(snapshot.number("fetched_at")),style=MaterialTheme.typography.bodySmall)
  if(state.usage?.text("error")?.isNotBlank()==true) Text(state.usage.text("error"),color=MaterialTheme.colorScheme.error)
  windows.forEach { window -> val used=window.number("used_percent"); Text("${window.text("label")} · ${used?.let { "${(100-it).coerceIn(0.0,100.0).toInt()}% remaining" } ?: "Unknown"}"); Text("Resets ${timeText(window.number("reset_at"))}",style=MaterialTheme.typography.bodySmall) }
  Action("View usage history",!state.busy) { controller.history("chatgpt") }
  Disclosure("About allowance") { Text("Codex reports allowance for the selected ChatGPT connection. Windows may not cover every ChatGPT feature. Last-known values are marked; failed refreshes do not mean zero allowance.") }
 }
}
@Composable private fun ResearchSettings(controller: AgentController,state: AgentState) {
 val settings=state.researchSettings
 var key by remember { mutableStateOf("") }
 Text("Brave Search",style=MaterialTheme.typography.titleMedium); Text(settings?.text("status") ?: "Status unavailable"); Text("Each research task needs your permission. Saving a key does not grant consent.")
 Field("Brave Search API key",key,{ key=it },true)
 Action("Verify and save key",!state.busy && settings!=null && key.isNotBlank()) { controller.researchSetting("verify",key); key="" }
 if(settings?.flag("configured")==true) { Action("Test saved key",!state.busy) { controller.testSavedResearchKey() }; Action("Remove key",!state.busy) { controller.researchSetting("remove") } }
 if(settings!=null) { val budget=settings.obj("budget"); var enabled by remember(budget) { mutableStateOf(budget.flag("enabled")) }; var maximum by remember(budget) { mutableStateOf(budget.text("maximum")) }; var period by remember(budget) { mutableStateOf(budget.text("period").ifBlank { "month" }) }; var zone by remember(budget) { mutableStateOf(budget.text("timezone").ifBlank { "UTC" }) }
  Row(verticalAlignment=Alignment.CenterVertically) { Switch(enabled,{ enabled=it },enabled=!state.busy); Text("Limit search requests") }
  Field("Maximum requests",maximum,{ maximum=it }); Row { Action(if(period=="month") "Calendar month ✓" else "Calendar month") { period="month" }; Action(if(period=="day") "Day ✓" else "Day") { period="day" } }; Field("Reset timezone",zone,{ zone=it }); Action("Save cap",!state.busy) { controller.budget(enabled,maximum,period,zone) }
 }
 Action("View search usage history",!state.busy) { controller.history("brave") }
}
@Composable private fun MemoryView(controller: AgentController,state: AgentState) {
 val memory=state.memory
 Row(verticalAlignment=Alignment.CenterVertically) { Switch(memory?.flag("enabled") ?: false,{ controller.memoryEnabled(it) },enabled=!state.busy && memory!=null); Text("Include saved memory in new chats",Modifier.weight(1f)) }
 if(memory==null) Text("Memory unavailable. Refresh settings to retry.")
 var content by remember { mutableStateOf("") }; var category by remember { mutableStateOf("preference") }; var editing by remember { mutableStateOf("") }
 memory?.rows("entries")?.forEach { entry -> Column { Text(entry.text("category"),color=MaterialTheme.colorScheme.primary); SelectionContainer { Text(entry.text("content")) }; Row { Action("Edit",!state.busy) { editing=entry.text("id"); content=entry.text("content"); category=entry.text("category") }; Action("Delete",!state.busy) { controller.deleteMemory(entry) } } } }
 Field("Category",category,{ category=it }); OutlinedTextField(content,{ content=it },label={ Text("Memory to save") },modifier=Modifier.fillMaxWidth(),maxLines=6)
 Action(if(editing.isEmpty()) "Save memory" else "Save changes",!state.busy && memory!=null && content.isNotBlank()) { controller.saveMemory(editing,category,content); editing=""; content="" }
 if(editing.isNotEmpty()) Action("Cancel edit") { editing=""; content="" }
}
@Composable private fun PasswordView(controller: AgentController,state: AgentState) {
 var current by remember { mutableStateOf("") }; var next by remember { mutableStateOf("") }; var confirm by remember { mutableStateOf("") }
 Text("Change your Agent password, not your ChatGPT password. Other Agent sessions will be signed out.")
 Field("Current password",current,{ current=it },true); Field("New password",next,{ next=it },true); Field("Confirm new password",confirm,{ confirm=it },true)
 Action("Update password",!state.busy && current.isNotEmpty() && next.isNotEmpty() && confirm.isNotEmpty()) { controller.password(current,next,confirm); current=""; next=""; confirm="" }
}
@Composable private fun HistoryView(controller: AgentController,state: AgentState) {
 LazyColumn(Modifier.fillMaxSize(),contentPadding=PaddingValues(16.dp),verticalArrangement=Arrangement.spacedBy(12.dp)) {
  item { Text(if(state.historyProvider=="brave") "Search usage history" else "Allowance history",style=MaterialTheme.typography.titleLarge); Row { listOf("24h","week","period").forEach { range -> TextButton(onClick={ controller.history(state.historyProvider,range,state.historyWindow) },enabled=!state.busy) { Text(range) } } }
   state.history?.rows("windows")?.forEach { window -> Action(window.text("label").ifBlank { window.text("id") },!state.busy) { controller.history(state.historyProvider,state.historyRange,window.text("id")) } }
   if(state.history==null) Text(if(state.busy) "Loading history…" else "History unavailable. Choose a range to retry.")
   state.history?.text("warning")?.takeIf { it.isNotBlank() }?.let { Text(it) }
   if(state.history!=null && state.history.rows("points").isEmpty()) Text("No recorded observations in this range.")
  }
  items(state.history?.rows("points").orEmpty()) { point -> Text("${timeText(point.number("at"))} · ${point.number("remaining_percent")?.let { "$it% remaining" } ?: "Unknown"}"); if(point.number("reset_at")!=null) Text("Reset ${timeText(point.number("reset_at"))}",style=MaterialTheme.typography.bodySmall) }
 }
}
