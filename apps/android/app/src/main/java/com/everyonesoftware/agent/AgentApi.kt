package com.everyonesoftware.agent

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.*
import okhttp3.*
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

val wireJson = Json { ignoreUnknownKeys = true }
fun parseObject(value: String) = wireJson.parseToJsonElement(value).jsonObject
fun JsonObject.text(key: String): String = (get(key) as? JsonPrimitive)?.contentOrNull ?: ""
fun JsonObject.flag(key: String) = (get(key) as? JsonPrimitive)?.booleanOrNull ?: false
fun JsonObject.number(key: String) = (get(key) as? JsonPrimitive)?.doubleOrNull
fun JsonObject.obj(key: String) = get(key) as? JsonObject ?: buildJsonObject {}
fun JsonObject.rows(key: String) = (get(key) as? JsonArray)?.mapNotNull { it as? JsonObject } ?: emptyList()
fun json(vararg values: Pair<String, Any?>): JsonObject = buildJsonObject { values.forEach { (k,v) -> put(k, when(v) { null -> JsonNull; is JsonElement -> v; is Boolean -> JsonPrimitive(v); is Number -> JsonPrimitive(v); else -> JsonPrimitive(v.toString()) }) } }
data class Session(val endpoint: String, val token: String)
interface SessionStore { fun read(): Session?; fun write(session: Session? ) }
class MemorySessionStore : SessionStore { private var session: Session? = null; override fun read() = session; override fun write(session: Session?) { this.session = session } }
class ApiFailure(val status: Int, val data: JsonObject) : IOException(data.text("error").ifBlank { "Request failed ($status)" })

class AgentApi(endpoint: String, private val store: SessionStore) {
 val base = endpoint.toHttpUrl()
 private var token: String? = store.read()?.takeIf { it.endpoint == base.toString() }?.token
 private val client = OkHttpClient.Builder().followRedirects(false).followSslRedirects(false).retryOnConnectionFailure(false).callTimeout(150, TimeUnit.SECONDS).build()
 suspend fun login(username: String, password: String, register: Boolean): JsonObject = request("POST", if(register) "/api/auth/register" else "/api/auth/login", json("username" to username, "password" to password))
 suspend fun get(path: String): JsonObject = request("GET", path)
 fun cancel() { client.dispatcher.cancelAll() }
 fun forget() { cancel(); token = null; store.write(null) }
 suspend fun request(method: String, path: String, body: JsonObject? = null, headers: Map<String,String> = emptyMap()): JsonObject = withContext(Dispatchers.IO) {
  require(path.startsWith("/") && !path.startsWith("//"))
  val url = requireNotNull(base.resolve(path))
  require(url.scheme == base.scheme && url.host == base.host && url.port == base.port)
  val builder = Request.Builder().url(url).header("Accept", "application/json").header("Cache-Control", "no-store")
  token?.let { builder.header("Cookie", "everyoneagent_session=$it") }
  headers.forEach { (k,v) -> builder.header(k,v) }
  builder.method(method, if(method == "GET" || method == "HEAD") null else (body?.toString() ?: "{}").toRequestBody("application/json".toMediaType()))
  val response = client.newCall(builder.build()).await()
  response.use {
   if (it.code in 300..399) throw IOException("Server redirects are not allowed. Enter the final HTTPS server address.")
   val data = it.body?.source()?.let { source -> source.request(4L * 1024 * 1024 + 1); require(source.buffer.size <= 4L * 1024 * 1024) { "Server response too large" }; source.readUtf8() } ?: "{}"
   val result = try { parseObject(data) } catch (_: Exception) { throw IOException("Server returned an invalid response (${it.code})") }
   if (!it.isSuccessful) throw ApiFailure(it.code, result)
   it.headers.values("Set-Cookie").mapNotNull { header -> Cookie.parse(url, header) }.firstOrNull { cookie -> cookie.name == "everyoneagent_session" }?.let { cookie ->
    token = cookie.value.takeIf { value -> value.isNotEmpty() && cookie.expiresAt > System.currentTimeMillis() }
    store.write(token?.let { value -> Session(base.toString(), value) })
   }
   result
  }
 }
}

suspend fun Call.await(): Response = suspendCancellableCoroutine { continuation ->
 continuation.invokeOnCancellation { cancel() }
 enqueue(object : Callback {
  override fun onFailure(call: Call, e: IOException) { if(!continuation.isCancelled) continuation.resumeWithException(IOException("Connection failed. Check your server and try again.", e)) }
  override fun onResponse(call: Call, response: Response) { if(continuation.isCancelled) response.close() else continuation.resume(response) }
 })
}

fun validatedEndpoint(value: String): String {
 val url = value.trim().toHttpUrl()
 require(url.username.isEmpty() && url.password.isEmpty() && url.query == null && url.fragment == null && url.encodedPath == "/") { "Use a server origin, without a path or credentials" }
 require(url.isHttps || url.host in setOf("localhost", "127.0.0.1", "::1")) { "HTTPS required except explicit loopback development" }
 return url.toString()
}
