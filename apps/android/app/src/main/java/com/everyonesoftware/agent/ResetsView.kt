package com.everyonesoftware.agent

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.focusable
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ExpandLess
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.History
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.*
import androidx.compose.ui.semantics.*
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import java.time.Instant

@Composable internal fun ResetsView(controller: AgentController, state: AgentState) {
 val now by produceState(Instant.now().epochSecond, state.usage) {
  while (true) { value = Instant.now().epochSecond; delay(1_000) }
 }
 val credits = remember(state.activeProfile, state.usage, now) { state.resetCredits(now) }
 val offer = controller.resetOffer(state, credits)
 val profile = state.activeProfile
 val connection = state.usage?.text("connection_id")
 var expanded by remember(profile, connection) { mutableStateOf(false) }
 var pending by remember { mutableStateOf<ResetOffer?>(null) }
 var restoreFocus by remember { mutableStateOf(false) }
 val launcherFocus = remember { FocusRequester() }
 val headingFocus = remember { FocusRequester() }
 val enabled = offer != null && !state.busy
 val confirmation = pending?.takeIf { it == offer && enabled }
 fun dismiss() { pending = null; restoreFocus = true }
 LaunchedEffect(pending, confirmation, enabled, state.busy, restoreFocus) {
  if (pending != null && confirmation == null) dismiss()
  if (restoreFocus && pending == null && !state.busy) {
   (if (enabled) launcherFocus else headingFocus).requestFocus()
   restoreFocus = false
  }
 }
 val scheme = MaterialTheme.colorScheme
 val type = MaterialTheme.typography
 HorizontalDivider()
 FlowRow(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, itemVerticalAlignment = Alignment.CenterVertically) {
  Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp),
   modifier = Modifier.focusRequester(headingFocus).focusable().semantics(mergeDescendants = true) { heading() }) {
   Icon(Icons.Default.History, null, Modifier.size(20.dp), tint = scheme.primary)
   Text("Resets", style = type.titleMedium)
  }
  Button(onClick = { pending = offer }, enabled = enabled, modifier = Modifier.focusRequester(launcherFocus)) { Text("Use a reset") }
 }
 if (credits == null) {
  Text("Reset balance unavailable", style = type.bodyMedium, modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite })
 } else {
  Surface(Modifier.fillMaxWidth(), shape = MaterialTheme.shapes.small, border = BorderStroke(1.dp, Color(0xff48564a)), color = Color(0xff202722)) {
   Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
    FlowRow(Modifier.semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite },
     horizontalArrangement = Arrangement.spacedBy(8.dp), itemVerticalAlignment = Alignment.CenterVertically) {
     Text(credits.count.toString(), color = scheme.primary, style = type.titleLarge)
     Text(if (credits.count == 1L) "reset available" else "resets available", style = type.bodyMedium)
    }
    if (credits.expirations.isNotEmpty()) HorizontalDivider(color = Color(0xff3b473d))
    credits.expirations.forEach { row ->
     FlowRow(Modifier.fillMaxWidth().semantics(mergeDescendants = true) { }, horizontalArrangement = Arrangement.SpaceBetween) {
      val label = when {
       row.at == null && row.count == 1 -> "1 reset without an expiration"
       row.at == null -> "${row.count} resets without an expiration"
       row.count == 1 -> "1 reset expires"
       else -> "${row.count} resets expire"
      }
      Text(label, style = type.bodySmall)
      if (row.at != null) Text(resetTime(row.at), style = type.bodySmall)
     }
    }
    if (credits.count > 0 && !credits.complete) Text("Some expiration details are unavailable.", style = type.bodySmall, color = scheme.onSurfaceVariant)
    else if (credits.count > 0 && offer == null) Text("A reset can’t be selected right now.", style = type.bodySmall, color = scheme.onSurfaceVariant)
   }
  }
 }
 if (state.resetFeedback.isNotBlank()) Text(state.resetFeedback, style = type.bodySmall,
  modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite })
 TextButton(onClick = { expanded = !expanded }, modifier = Modifier.fillMaxWidth().semantics {
  stateDescription = if (expanded) "Expanded" else "Collapsed"
 }) {
  Text("Reset history", Modifier.weight(1f), style = type.labelLarge)
  Icon(if (expanded) Icons.Default.ExpandLess else Icons.Default.ExpandMore, null)
 }
 if (expanded) {
  if (credits == null || credits.historyUnavailable) Text("Reset history unavailable.", style = type.bodySmall)
  else if (credits.history.isEmpty()) Text("No reset events recorded by Agent.", style = type.bodySmall)
  credits?.history?.forEach { event ->
   val used = event.kind == "used"
   HorizontalDivider()
   Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
    Column(Modifier.weight(1f)) {
     Text(if (used) "1 reset used" else "1 reset expired", style = type.bodyMedium)
     Text(resetTime(event.at), style = type.bodySmall, color = scheme.onSurfaceVariant)
    }
    val tone = if (used) scheme.primary else Color(0xffe3c59a)
    Surface(shape = MaterialTheme.shapes.extraLarge, border = BorderStroke(1.dp, tone.copy(alpha = 0.38f)), color = Color.Transparent) {
     Text(if (used) "Used" else "Expired", Modifier.padding(horizontal = 8.dp, vertical = 3.dp).clearAndSetSemantics { },
      style = type.labelSmall, color = tone)
    }
   }
  }
 }
 confirmation?.let { confirmed ->
  val cancelFocus = remember { FocusRequester() }
  val expiry = confirmed.credit.expiresAt?.let { "that expires ${resetTime(it)}" } ?: "with no expiration"
  val remaining = confirmed.count - 1
  val left = if (remaining == 1L) "1 reset" else "$remaining resets"
  val recovery = if (confirmed.credit.pendingAttemptId != null) " A previous attempt for this reset couldn’t be verified. This will retry that same attempt." else ""
  AlertDialog(onDismissRequest = ::dismiss,
   modifier = Modifier.onPreviewKeyEvent { event ->
    if (event.key != Key.Escape) false else { if (event.type == KeyEventType.KeyUp) dismiss(); true }
   },
   title = { Text("Use 1 reset?") },
   text = { Text("This will use the reset $expiry for ${confirmed.account}. You’ll have $left left. This can’t be undone.$recovery",
    Modifier.verticalScroll(rememberScrollState())) },
   dismissButton = { OutlinedButton(onClick = ::dismiss, modifier = Modifier.focusRequester(cancelFocus)) { Text("Cancel") } },
   confirmButton = { Button(onClick = { dismiss(); controller.consumeReset(confirmed) }) { Text("Use reset") } },
  )
  LaunchedEffect(Unit) { cancelFocus.requestFocus() }
 }
}
