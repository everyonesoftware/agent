package com.everyonesoftware.agent

import kotlinx.coroutines.*
import kotlinx.serialization.json.*
import okhttp3.*
import okhttp3.HttpUrl.Companion.toHttpUrl
import java.io.File
import java.io.IOException
import java.security.MessageDigest
import java.util.concurrent.TimeUnit

private const val REPO = "everyonesoftware/agent"
private val shaPattern = Regex("[0-9a-f]{40}")
private val digestPattern = Regex("[0-9a-f]{64}")
fun sha256(bytes: ByteArray): String = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it.toInt() and 255) }
private fun JsonObject.integer(name: String): Long {
 val value = get(name) as? JsonPrimitive
 require(value != null && !value.isString && Regex("[0-9]+").matches(value.content)) { "Invalid numeric metadata" }
 return requireNotNull(value.content.toLongOrNull())
}
data class UpdateOffer(val code: Int, val version: String, val commit: String, val size: Long, val hash: String, val signer: String, val url: String)
fun parseReleaseManifest(data: JsonObject, code: Int, url: String, assetSize: Long): UpdateOffer {
 require(data.integer("schemaVersion") == 1L && data.text("platform") == "android")
 require(data.integer("versionCode") == code.toLong() && data.text("versionName") == "0.1.$code")
 require(code > 0 && shaPattern.matches(data.text("commit")))
 val apk = data.obj("apk")
 require(apk.text("name") == "Agent-android.apk" && apk.integer("size") == assetSize && assetSize in 1..536870912)
 require(digestPattern.matches(apk.text("sha256")) && digestPattern.matches(data.text("signingCertificateSha256")))
 return UpdateOffer(code,"0.1.$code",data.text("commit"),assetSize,apk.text("sha256"),data.text("signingCertificateSha256"),url)
}
data class Change(val sha: String, val subject: String, val bullet: String, val fix: Boolean, val visible: Boolean)
fun groupChange(sha: String, subject: String): Change {
 val clean = subject.replace(Regex("^\\[verified]\\s*", RegexOption.IGNORE_CASE), "")
 val scoped = Regex("^([a-z]+)(?:\\(([^)]+)\\))?!?:\\s*(.*)$").matchEntire(clean)
 val type = scoped?.groupValues?.get(1).orEmpty()
 val scope = scoped?.groupValues?.get(2).orEmpty()
 val visible = type !in setOf("docs","ci") && scope !in setOf("windows","winui","ci","docs")
 return Change(sha,subject,scoped?.groupValues?.get(3) ?: clean,type == "fix" || type == "revert",visible)
}

/** Independent unauthenticated transport; production URLs are fixed and checked before every hop. */
class UpdateClient internal constructor(private val testOrigin: HttpUrl?) {
 constructor() : this(null)
 init { testOrigin?.let { require(!it.isHttps && it.host in setOf("localhost","127.0.0.1","::1") && it.encodedPath == "/") } }
 private val http = OkHttpClient.Builder().cookieJar(CookieJar.NO_COOKIES).authenticator(Authenticator.NONE).proxyAuthenticator(Authenticator.NONE)
  .followRedirects(false).followSslRedirects(false).retryOnConnectionFailure(false).callTimeout(120,TimeUnit.SECONDS).build()
 private fun api(path: String) = "https://api.github.com/repos/$REPO/$path"
 private fun asset(code: Int, name: String) = "https://github.com/$REPO/releases/download/v0.1.$code/$name"
 fun cancel() = http.dispatcher.cancelAll()
 private suspend fun fetch(url: String, download: Boolean = false): Response {
  var target = url.toHttpUrl()
  require(target.isHttps && target.username.isEmpty() && target.password.isEmpty() && target.fragment == null)
  require(if(download) target.host == "github.com" && target.encodedPath.startsWith("/$REPO/releases/download/") && target.query == null else target.host == "api.github.com" && target.encodedPath.startsWith("/repos/$REPO/"))
  repeat(4) { hop ->
   val actual = testOrigin?.resolve(target.encodedPath + (target.encodedQuery?.let { "?$it" } ?: "")) ?: target
   val response = http.newCall(Request.Builder().url(actual).header("Accept", if(download) "application/octet-stream" else "application/vnd.github+json").header("User-Agent","Agent-Android").build()).await()
   if(response.code in 300..399) {
    val location = response.header("Location"); response.close()
    require(download && testOrigin == null && hop < 3)
    target = requireNotNull(target.resolve(requireNotNull(location)))
    require(target.isHttps && target.port == 443 && target.username.isEmpty() && target.password.isEmpty() && target.fragment == null && target.host in setOf("release-assets.githubusercontent.com","objects.githubusercontent.com","github.com"))
   } else {
    if(!response.isSuccessful) { val code = response.code; response.close(); throw IOException("GitHub update request failed ($code); retry later") }
    return response
   }
  }
  throw IOException("Invalid update redirect")
 }
 private suspend fun document(url: String, download: Boolean = false): JsonElement = fetch(url,download).use { response ->
  val source = requireNotNull(response.body).source(); source.request(4L*1024*1024+1)
  require(source.buffer.size <= 4L*1024*1024) { "Update metadata too large" }
  wireJson.parseToJsonElement(source.readUtf8())
 }
 private suspend fun inventory(): List<JsonObject> {
  val all = mutableListOf<JsonObject>()
  for(page in 1..10) {
   val entries = fetch(api("releases?per_page=100&page=$page")).use { response ->
    val source = requireNotNull(response.body).source(); source.request(4L*1024*1024+1); require(source.buffer.size <= 4L*1024*1024)
    val rows = wireJson.parseToJsonElement(source.readUtf8()).jsonArray.map { it.jsonObject }
    require(rows.size <= 100)
    require(rows.size == 100 || !response.headers.values("Link").any { it.contains("rel=\"next\"") }) { "Incomplete release inventory" }
    rows
   }
   all += entries
   if(entries.size < 100) return all
  }
  throw IOException("Release inventory incomplete; retry later")
 }
 suspend fun latest(installedCode: Int): UpdateOffer? = withContext(Dispatchers.IO) {
  val candidates = inventory().filter { !it.flag("draft") && !it.flag("prerelease") && it.text("published_at").isNotEmpty() && it.rows("assets").any { a -> a.text("name") == "Agent-android.apk" || a.text("name") == "agent-android-release.json" } }
  val numbered = candidates.map { release ->
   val match = requireNotNull(Regex("v0\\.1\\.([1-9][0-9]*)").matchEntire(release.text("tag_name"))) { "Invalid Android release version" }
   requireNotNull(match.groupValues[1].toIntOrNull()) to release
  }
  require(numbered.map { it.first }.distinct().size == numbered.size) { "Duplicate Android releases" }
  val highest = numbered.maxByOrNull { it.first } ?: return@withContext null
  if(highest.first <= installedCode) return@withContext null
  readOffer(highest.second,highest.first)
 }
 private suspend fun readOffer(release: JsonObject, code: Int): UpdateOffer {
  val assets = release.rows("assets")
  val apk = assets.single { it.text("name") == "Agent-android.apk" }
  val metadata = assets.single { it.text("name") == "agent-android-release.json" }
  require(apk.text("browser_download_url") == asset(code,"Agent-android.apk") && metadata.text("browser_download_url") == asset(code,"agent-android-release.json"))
  return parseReleaseManifest(document(metadata.text("browser_download_url"),true).jsonObject,code,apk.text("browser_download_url"),apk.integer("size"))
 }
 suspend fun changes(installedCode: Int, installedCommit: String, offer: UpdateOffer): List<Change> = withContext(Dispatchers.IO) {
  require(shaPattern.matches(installedCommit) && shaPattern.matches(offer.commit) && offer.code > installedCode)
  val installedRelease = inventory().single { it.text("tag_name") == "v0.1.$installedCode" && !it.flag("draft") && !it.flag("prerelease") }
  require(readOffer(installedRelease,installedCode).commit == installedCommit) { "Installed source is not a verified published Android build" }
  val result = mutableListOf<Change>(); var total: Int? = null
  for(page in 1..10) {
   val data = document(api("compare/$installedCommit...${offer.commit}?per_page=100&page=$page")).jsonObject
   require(data.text("status") == "ahead" && data.obj("base_commit").text("sha") == installedCommit)
   val expected = data.integer("total_commits").toInt(); require(expected in 1..1000 && (total == null || total == expected)); total = expected
   val commits = data.rows("commits")
   commits.forEach { c -> require(shaPattern.matches(c.text("sha"))); result += groupChange(c.text("sha"),c.obj("commit").text("message").lineSequence().first()) }
   require(result.map { it.sha }.distinct().size == result.size && result.size <= expected)
   if(result.size == expected) { require(result.last().sha == offer.commit); return@withContext result }
   require(commits.size == 100) { "Incomplete change history" }
  }
  throw IOException("Change history incomplete")
 }
 suspend fun download(offer: UpdateOffer, target: File, progress: (Float) -> Unit) = withContext(Dispatchers.IO) {
  require(offer.url == asset(offer.code,"Agent-android.apk"))
  require(offer.size in 1..536870912 && digestPattern.matches(offer.hash))
  var ownsTarget=false
  try {
   require(target.createNewFile())
   ownsTarget=true
   fetch(offer.url,true).use { response ->
    val body = requireNotNull(response.body); require(body.contentLength() == -1L || body.contentLength() == offer.size)
    val digest = MessageDigest.getInstance("SHA-256"); var received = 0L
    body.byteStream().use { input -> target.outputStream().use { output ->
     val buffer = ByteArray(32768)
     while(true) { currentCoroutineContext().ensureActive(); val n = input.read(buffer); if(n < 0) break; received += n; require(received <= offer.size); output.write(buffer,0,n); digest.update(buffer,0,n); progress(received.toFloat()/offer.size) }
    } }
    require(received == offer.size && digest.digest().joinToString("") { "%02x".format(it.toInt() and 255) } == offer.hash) { "Update integrity verification failed" }
   }
  } catch(t: Throwable) { if(ownsTarget) target.delete(); throw t }
 }
}
