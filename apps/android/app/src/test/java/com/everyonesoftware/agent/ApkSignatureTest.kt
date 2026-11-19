package com.everyonesoftware.agent

import org.junit.Assert.*
import org.junit.Test
import java.io.File

class ApkSignatureTest {
 @Test fun cryptographicVerificationRejectsModifiedApkEvenWhenManifestHashMatches() {
  val file=File.createTempFile("agent-signature", ".apk")
  try {
   javaClass.getResourceAsStream("/valid-update.apk")!!.use { input -> file.outputStream().use { input.copyTo(it) } }
   val signer=verifiedApkSigners(file,26)
   assertEquals(1,signer.size)
   // Change signed ZIP payload, not signer metadata. A recomputed download hash cannot bless it.
   val bytes=file.readBytes()
   val needle="agent-signature-fixture".toByteArray()
   val position=(0..bytes.size-needle.size).first { offset -> needle.indices.all { bytes[offset+it]==needle[it] } }
   bytes[position]='X'.code.toByte(); file.writeBytes(bytes)
   assertThrows(Exception::class.java) { verifiedApkSigners(file,26) }
  } finally { file.delete() }
 }
}
