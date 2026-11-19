package com.everyonesoftware.agent

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class SecureSessionTest {
 @Test fun sessionReopensEncryptedAndClearsWithoutPasswordPersistence() {
  val context = InstrumentationRegistry.getInstrumentation().targetContext
  val store = SecureSessionStore(context)
  store.write(Session("https://test.invalid/", "test-opaque-session"))
  assertEquals("test-opaque-session", SecureSessionStore(context).read()?.token)
  val bytes = context.getSharedPreferences("agent_session", 0).all.toString()
  assertFalse(bytes.contains("test-opaque-session"))
  store.write(null)
  assertNull(SecureSessionStore(context).read())
 }
}
