package com.everyonesoftware.agent

import java.io.File
import java.nio.file.Files
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class UpdateClientTest {
 private fun manifest(code: Int) = json("schemaVersion" to 1, "platform" to "android", "versionName" to "0.1.$code", "versionCode" to code, "commit" to "a".repeat(40), "apk" to json("name" to "Agent-android.apk", "size" to 3, "sha256" to "b".repeat(64)), "signingCertificateSha256" to "c".repeat(64))
 private fun release(code: Int, android: Boolean = true) = json("tag_name" to "v0.1.$code", "published_at" to "2026-11-19T00:00:00Z", "draft" to false, "prerelease" to false, "assets" to JsonArray(if(android) listOf("Agent-android.apk", "agent-android-release.json").map { name -> json("name" to name, "size" to if(name.endsWith(".apk")) 3 else 500, "browser_download_url" to "https://github.com/everyonesoftware/agent/releases/download/v0.1.$code/$name") } else emptyList()))
 @Test fun androidDiscoveryIgnoresNewerWindowsAndValidatesExactSchema() = runBlocking {
  MockWebServer().use { server ->
   server.enqueue(MockResponse().setBody(JsonArray(listOf(release(102,false),release(101),release(99))).toString()))
   server.enqueue(MockResponse().setBody(manifest(101).toString()))
   val offer = UpdateClient(server.url("/")).latest(100)!!
   assertEquals(101,offer.code); assertEquals("a".repeat(40),offer.commit)
   assertEquals("/repos/everyonesoftware/agent/releases?per_page=100&page=1",server.takeRequest().path)
   assertNull(server.takeRequest().getHeader("Cookie"))
  }
 }
 @Test fun highestInvalidManifestNeverFallsBack() = runBlocking {
  MockWebServer().use { server ->
   server.enqueue(MockResponse().setBody(JsonArray(listOf(release(101),release(102))).toString()))
   server.enqueue(MockResponse().setBody(manifest(101).toString()))
   try { UpdateClient(server.url("/")).latest(100); fail("accepted mismatched highest candidate") } catch (_: IllegalArgumentException) {}
   assertEquals(2,server.requestCount)
  }
 }
 @Test fun failedFileClaimNeverDeletesAnotherDownload() = runBlocking {
  val directory = Files.createTempDirectory("agent-update-test").toFile()
  try {
   val target = File(directory,"update.apk").apply { writeText("replacement") }
   val offer = UpdateOffer(101,"0.1.101","a".repeat(40),3,sha256("new".toByteArray()),"b".repeat(64),"https://github.com/everyonesoftware/agent/releases/download/v0.1.101/Agent-android.apk")
   try { UpdateClient().download(offer,target) {}; fail("accepted an occupied target") } catch (_: IllegalArgumentException) {}
   assertTrue("A failed file claim must not delete another attempt's file",target.exists())
   assertEquals("replacement",target.readText())
  } finally { directory.deleteRecursively() }
 }
 @Test fun manifestRejectsStringNumbersAndNonCanonicalIdentity() {
  val invalid = listOf(JsonObject(manifest(101)+("versionCode" to JsonPrimitive("101"))), JsonObject(manifest(101)+("platform" to JsonPrimitive("windows"))), JsonObject(manifest(101)+("commit" to JsonPrimitive("main"))))
  invalid.forEach { value -> assertThrows(IllegalArgumentException::class.java) { parseReleaseManifest(value,101,"url",3) } }
 }
}
