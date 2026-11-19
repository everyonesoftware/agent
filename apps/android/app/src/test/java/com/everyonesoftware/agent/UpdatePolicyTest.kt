package com.everyonesoftware.agent

import org.junit.Assert.*
import org.junit.Test

class UpdatePolicyTest {
 private val offer=UpdateOffer(101,"0.1.101","a".repeat(40),3,sha256(byteArrayOf(1,2,3)),sha256(byteArrayOf(9)),"url")
 private val installed=ApkIdentity("com.everyonesoftware.agent",100,"0.1.100",false,listOf(byteArrayOf(9)))
 private val archive=installed.copy(code=101,version="0.1.101")
 @Test fun sameSignerHigherVersionIsAccepted() { verifyApkPolicy(installed,archive,offer) }
 @Test fun identitySignerVersionAndDebugRejectionFailClosed() {
  val invalid=listOf(archive.copy(packageName="other"),archive.copy(code=99),archive.copy(code=102),archive.copy(version="0.1.102"),archive.copy(debuggable=true),archive.copy(signers=emptyList()),archive.copy(signers=listOf(byteArrayOf(8))))
  invalid.forEach { candidate -> assertThrows(IllegalArgumentException::class.java) { verifyApkPolicy(installed,candidate,offer) } }
 }
 @Test fun changelogStripsPrefixesAndRetainsOriginalEvidence() {
  val change=groupChange("a".repeat(40),"[verified] fix(android): Repair session restore")
  assertEquals("Repair session restore",change.bullet); assertTrue(change.fix); assertTrue(change.visible)
  assertEquals("[verified] fix(android): Repair session restore",change.subject)
  assertFalse(groupChange("sha","docs: clarify setup").visible)
  assertFalse(groupChange("sha","fix(windows): title bar").visible)
  assertTrue(groupChange("sha","fix: shared authentication").visible)
 }
}
