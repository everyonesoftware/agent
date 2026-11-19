package com.everyonesoftware.agent

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Only the origin and session cookie are persisted. Never passwords/provider credentials. */
class SecureSessionStore(context: Context) : SessionStore {
 private val prefs = context.getSharedPreferences("agent_session", Context.MODE_PRIVATE)
 private fun key(): SecretKey {
  val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
  (store.getKey("agent.session.v1", null) as? SecretKey)?.let { return it }
  return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
   init(KeyGenParameterSpec.Builder("agent.session.v1", KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
    .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
  }.generateKey()
 }
 @Synchronized override fun read(): Session? {
  val blob = prefs.getString("sealed", null) ?: return null
  return try {
   val bytes = Base64.decode(blob, Base64.NO_WRAP)
   val cipher = Cipher.getInstance("AES/GCM/NoPadding")
   cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, bytes.copyOfRange(0,12)))
   cipher.updateAAD("Agent session v1".toByteArray())
   val data = parseObject(String(cipher.doFinal(bytes.copyOfRange(12,bytes.size)), Charsets.UTF_8))
   Session(validatedEndpoint(data.text("endpoint")), data.text("token")).takeIf { it.token.isNotEmpty() }
  } catch (_: Exception) { prefs.edit().clear().commit(); null }
 }
 @Synchronized override fun write(session: Session?) {
  if(session == null) { check(prefs.edit().clear().commit()); return }
  val cipher = Cipher.getInstance("AES/GCM/NoPadding")
  cipher.init(Cipher.ENCRYPT_MODE, key()); cipher.updateAAD("Agent session v1".toByteArray())
  val ciphertext = cipher.doFinal(json("endpoint" to session.endpoint, "token" to session.token).toString().toByteArray())
  check(prefs.edit().putString("sealed", Base64.encodeToString(cipher.iv + ciphertext, Base64.NO_WRAP)).commit()) { "Session could not be stored securely" }
 }
}
