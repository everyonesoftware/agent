package com.everyonesoftware.agent

import kotlinx.serialization.json.*
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import java.util.UUID

internal data class ResetExpiration(val count: Int, val at: Long?)
internal data class ResetCredit(val id: String, val expiresAt: Long?, val pendingAttemptId: String? = null)
internal data class ResetEvent(val kind: String, val at: Long)
internal data class ResetCredits(
 val count: Long,
 val expirations: List<ResetExpiration>,
 val complete: Boolean,
 val next: ResetCredit?,
 val history: List<ResetEvent>,
 val historyUnavailable: Boolean,
)
internal data class ResetOffer(
 val target: ResetTarget,
 val account: String,
 val credit: ResetCredit,
 val count: Long,
)
internal data class ResetTarget(val profile: String, val connection: String, val credit: String)

private fun JsonObject.timestamp(key: String): Long? = (get(key) as? JsonPrimitive)
 ?.takeUnless { it.isString }?.longOrNull?.takeIf { it in 1..253402300799L }
private fun JsonObject.count(key: String): Int? = (get(key) as? JsonPrimitive)
 ?.takeUnless { it.isString }?.intOrNull?.takeIf { it >= 0 }
internal val AgentState.activeProfile: String get() = profiles?.text("active_profile_id").orEmpty()
internal fun JsonObject.profileLabel() = text("label").ifBlank { text("email").ifBlank { text("id") } }

private fun JsonElement.validResetAttemptId(): String? {
 val id = (this as? JsonPrimitive)?.takeIf { it.isString }?.content ?: return null
 return id.takeIf { it.length == 36 && it != "00000000-0000-0000-0000-000000000000" &&
  runCatching { UUID.fromString(it).toString() }.getOrNull() == it }
}

internal fun AgentState.resetCredits(now: Long): ResetCredits? {
 val status = usage ?: return null
 if (activeProfile.isBlank() || status.text("profile_id") != activeProfile || status.text("connection_id").isBlank() ||
  status.text("state") != "connected" || !status.flag("available") || status.text("identity_match") == "different" || status.text("error").isNotBlank() ||
  status.obj("snapshot").flag("stale")) return null
 val data = status["resets"] as? JsonObject ?: return null
 val count = (data["available_count"] as? JsonPrimitive)?.takeUnless { it.isString }?.longOrNull?.takeIf { it >= 0 } ?: return null
 val rows = data["expirations"] as? JsonArray ?: return null
 val expirations = rows.map { raw ->
  val row = raw as? JsonObject ?: return null
  val amount = row.count("count")?.takeIf { it > 0 } ?: return null
  val at = if (row["expires_at"] == JsonNull) null else row.timestamp("expires_at")?.takeIf { it > now } ?: return null
  ResetExpiration(amount, at)
 }.sortedWith(compareBy { it.at ?: Long.MAX_VALUE })
 val described = expirations.sumOf { it.count.toLong() }
 if (described > count) return null
 val complete = data.flag("details_complete") && described == count
 val next = if (complete && count > 0) (data["next"] as? JsonObject)?.let nextCredit@{ row ->
  val id = (row["credit_id"] as? JsonPrimitive)?.takeIf { it.isString }?.content?.takeIf { it.isNotBlank() }
  val at = row.timestamp("expires_at")
  val pendingAttempt = row["pending_attempt_id"]?.let { it.validResetAttemptId() ?: return@nextCredit null }
  if (id != null && id == id.trim() && id.toByteArray().size <= 1024 && id.none { it.isISOControl() } &&
   (at != null || row["expires_at"] == JsonNull) && at == expirations.firstOrNull()?.at) ResetCredit(id, at, pendingAttempt) else null
 } else null
 val historyRows = data["history"] as? JsonArray
 val history = data.rows("history").mapNotNull { row ->
  val at = row.timestamp("at") ?: return@mapNotNull null
  val kind = row.text("kind").takeIf { it == "used" || it == "expired" } ?: return@mapNotNull null
  ResetEvent(kind, at)
 }.sortedByDescending { it.at }
 return ResetCredits(count, expirations, complete, next, history,
  data.flag("history_unavailable") || historyRows?.size != history.size)
}

internal fun AgentState.resetOffer(credits: ResetCredits?, blocked: Set<ResetTarget>): ResetOffer? {
 if (screen != "settings" || user == null || deviceLogin != null || credits?.historyUnavailable != false) return null
 val next = credits.next ?: return null
 val account = profiles?.rows("profiles")?.singleOrNull { it.text("id") == activeProfile && !it.flag("registration_pending") }?.profileLabel() ?: return null
 val target = ResetTarget(activeProfile, usage?.text("connection_id").orEmpty(), next.id)
 if (target.connection.isBlank() || target in blocked) return null
 return ResetOffer(target, account, next, credits.count)
}

private val eastern = ZoneId.of("America/New_York")
private val resetDate = DateTimeFormatter.ofPattern("MMM d, uuuu 'at' h:mm a 'ET'", Locale.US).withZone(eastern)
private val renewalDate = DateTimeFormatter.ofPattern("MMM d, h:mm a", Locale.US).withZone(eastern)
internal fun resetTime(seconds: Long) = resetDate.format(Instant.ofEpochSecond(seconds))
internal fun renewalTime(seconds: Double?): String? = seconds?.takeIf { it in 1.0..253402300799.0 }
 ?.let { renewalDate.format(Instant.ofEpochSecond(it.toLong())) }
