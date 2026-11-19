package com.everyonesoftware.agent

import kotlinx.coroutines.runBlocking
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class AgentApiTest {
 @Test fun loginStoresOnlyCookieAndUsesItForMe() = runBlocking {
  MockWebServer().use { server ->
   server.enqueue(MockResponse().setHeader("Set-Cookie", "everyoneagent_session=opaque; Path=/; HttpOnly").setBody("{\"id\":\"u\",\"username\":\"test\"}"))
   server.enqueue(MockResponse().setBody("{\"id\":\"u\",\"username\":\"test\"}"))
   val store = MemorySessionStore()
   val api = AgentApi(server.url("/").toString(), store)
   api.login("test", "not-a-real-password", false)
   assertEquals("test", api.get("/api/auth/me").text("username"))
   val login = server.takeRequest()
   assertEquals("/api/auth/login", login.path)
   assertEquals("not-a-real-password", parseObject(login.body.readUtf8()).text("password"))
   assertEquals("everyoneagent_session=opaque", server.takeRequest().getHeader("Cookie"))
   assertEquals("opaque", store.read()?.token)
  }
 }
}
