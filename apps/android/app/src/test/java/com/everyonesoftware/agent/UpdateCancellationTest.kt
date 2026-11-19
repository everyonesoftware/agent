package com.everyonesoftware.agent

import java.io.File
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.*
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

@OptIn(ExperimentalCoroutinesApi::class)
class UpdateCancellationTest {
 @get:Rule val temporary = TemporaryFolder()

 @Test fun replacementWaitsForCancelledDownloadAndItsLateCleanup() = runTest {
  MockWebServer().use { server ->
   val code = BuildConfig.VERSION_CODE + 1
   val root = "https://github.com/everyonesoftware/agent/releases/download/v0.1.$code/"
   server.enqueue(MockResponse().setBody("""[{"tag_name":"v0.1.$code","published_at":"2026-11-19T00:00:00Z","draft":false,"prerelease":false,"assets":[{"name":"Agent-android.apk","size":3,"browser_download_url":"${root}Agent-android.apk"},{"name":"agent-android-release.json","browser_download_url":"${root}agent-android-release.json"}]}]"""))
   server.enqueue(MockResponse().setBody("""{"schemaVersion":1,"platform":"android","versionCode":$code,"versionName":"0.1.$code","commit":"${"a".repeat(40)}","apk":{"name":"Agent-android.apk","size":3,"sha256":"${sha256("new".toByteArray())}"},"signingCertificateSha256":"${"b".repeat(64)}"}"""))
   val target = File(temporary.root,"updates/update.apk")
   val started = CompletableDeferred<Unit>()
   val cleanupEntered = CompletableDeferred<Unit>()
   val releaseCleanup = CompletableDeferred<Unit>()
   val cleanupFinished = CompletableDeferred<Unit>()
   var attempts = 0
   val manager = UpdateManager(backgroundScope,UpdateClient(server.url("/")),target,
    downloadFile = { _, file, progress ->
     attempts++
     if(attempts == 1) {
      file.writeText("old")
      started.complete(Unit)
      try { awaitCancellation() }
      finally {
       withContext(NonCancellable) {
        cleanupEntered.complete(Unit)
        releaseCleanup.await()
        file.delete()
        progress(0.9f) // A final callback from an already cancelled transfer.
        cleanupFinished.complete(Unit)
       }
      }
     } else { file.writeText("new"); progress(1f) }
    },
    verifyFile = { file, _ -> assertEquals("new",file.readText()) })
   manager.check()
   manager.state.first { it.offer != null }
   runCurrent()
   manager.download()
   started.await()
   manager.cancelDownload()
   cleanupEntered.await()
   try {
    manager.download() // Immediate replacement while the old finally is blocked.
    manager.check()
    manager.cancelDownload() // Repeated cancellation must not schedule more deletion.
    runCurrent()
    assertEquals("Downloading…",manager.state.value.phase)
    assertEquals(1,attempts)
   } finally { releaseCleanup.complete(Unit) }
   cleanupFinished.await()
   manager.state.first { it.phase == "Update available" }
   runCurrent()
   assertFalse(target.exists())
   assertEquals(0f,manager.state.value.progress)
   assertEquals("Download cancelled",manager.state.value.error)
   manager.download()
   manager.state.first { it.phase == "Ready to install" || it.phase == "Update failed" }
   runCurrent()
   assertEquals(2,attempts)
   assertEquals("Ready to install",manager.state.value.phase)
   assertEquals("new",target.readText())
   assertEquals(1f,manager.state.value.progress)
  }
 }
}
