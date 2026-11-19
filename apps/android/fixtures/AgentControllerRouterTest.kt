package com.everyonesoftware.agent

import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test

/** Only run by controller-router-tests.mjs with its task-owned real Go router. */
class AgentControllerRouterTest {
 private fun exercise(disconnect: Boolean) = runBlocking {
  val endpoint=requireNotNull(System.getenv("AGENT_CONTROLLER_ROUTER"))
  val controller=AgentController(MemorySessionStore(),CoroutineScope(coroutineContext+SupervisorJob()))
  suspend fun idle() { yield(); withTimeout(30000) { while(controller.state.value.busy) delay(5) } }
  try {
   controller.login(endpoint,"confirmation-user","correct-horse-battery",false); idle()
   assertNotNull(controller.state.value.user)
   controller.navigate("settings"); idle()
   val profiles=requireNotNull(controller.state.value.profiles)
   val active=profiles.text("active_profile_id")
   val target=if(disconnect) active else profiles.rows("profiles").single { it.text("id")!=active }.text("id")
   controller.changeProfile(target,"correct-horse-battery",disconnect); idle()
   assertEquals("Protected ${if(disconnect) "disconnect" else "switch"} failed: ${controller.state.value.error}","",controller.state.value.error)
   assertEquals(target,controller.state.value.profiles?.text("active_profile_id"))
   if(disconnect) {
    assertFalse(controller.state.value.status!!.flag("connected"))
    assertTrue(controller.state.value.profiles!!.rows("profiles").any { it.text("id")==target })
    assertTrue(controller.state.value.notice.contains("Tokens cleared"))
   }
  } finally { controller.close() }
 }
 @Test fun protectedSwitchReachesRealRouterAndReadsBackSelectedAccount() = exercise(false)
 @Test fun protectedDisconnectReachesRealRouterAndPreservesSavedAccount() = exercise(true)
}
