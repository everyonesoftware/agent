package com.everyonesoftware.agent

import com.android.apksig.ApkVerifier
import java.io.File

/** Verify signed payload bytes, not just PackageManager's archive metadata. */
internal fun verifiedApkSigners(file: File, minimumPlatform: Int): List<ByteArray> {
 val result = ApkVerifier.Builder(file).setMinCheckedPlatformVersion(minimumPlatform).build().verify()
 require(result.isVerified) { "APK signature verification failed" }
 return result.signerCertificates.map { it.encoded }.also {
  require(it.size == 1 && it.single().isNotEmpty()) { "APK must have one verified signer" }
 }
}
