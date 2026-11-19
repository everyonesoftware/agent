package com.everyonesoftware.agent

import android.content.*
import android.content.pm.*
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import java.io.File

internal data class ApkIdentity(val packageName: String,val code: Int,val version: String?,val debuggable: Boolean,val signers: List<ByteArray>)
internal fun verifyApkPolicy(installed: ApkIdentity, archive: ApkIdentity, offer: UpdateOffer) {
 require(installed.packageName=="com.everyonesoftware.agent" && archive.packageName==installed.packageName)
 require(archive.code==offer.code && archive.code>installed.code && archive.version==offer.version)
 require(!archive.debuggable || installed.debuggable)
 require(installed.signers.isNotEmpty() && archive.signers.isNotEmpty() && installed.signers.all { it.isNotEmpty() })
 val current=installed.signers.map(::sha256).sorted(); val next=archive.signers.map(::sha256).sorted()
 require(current==next && next.size==1 && next.single()==offer.signer) { "Update signer differs from installed app" }
}
class UpdateInstaller(private val context: Context) {
 @Suppress("DEPRECATION") private fun identity(file: File?): ApkIdentity {
  val flags=if(Build.VERSION.SDK_INT>=28) PackageManager.GET_SIGNING_CERTIFICATES else PackageManager.GET_SIGNATURES
  val info=requireNotNull(if(file==null) context.packageManager.getPackageInfo(context.packageName,flags) else context.packageManager.getPackageArchiveInfo(file.path,flags))
  val signers=if(Build.VERSION.SDK_INT>=28) info.signingInfo?.apkContentsSigners else info.signatures
  return ApkIdentity(info.packageName,if(Build.VERSION.SDK_INT>=28) info.longVersionCode.toInt() else info.versionCode,info.versionName,requireNotNull(info.applicationInfo).flags and ApplicationInfo.FLAG_DEBUGGABLE != 0,requireNotNull(signers).map { it.toByteArray() })
 }
 fun verify(file: File, offer: UpdateOffer) {
  val expected=File(context.cacheDir.canonicalFile,"updates/update.apk")
  require(file.canonicalFile==expected && expected.canonicalFile==expected && file.isFile && file.length()==offer.size)
  val verified=verifiedApkSigners(file,Build.VERSION.SDK_INT)
  val archive=identity(file)
  require(verified.map(::sha256).sorted()==archive.signers.map(::sha256).sorted()) { "APK signer metadata differs from verified signer" }
  verifyApkPolicy(identity(null),archive.copy(signers=verified),offer)
  val digest=java.security.MessageDigest.getInstance("SHA-256")
  file.inputStream().use { input -> val bytes=ByteArray(32768); while(true) { val count=input.read(bytes); if(count<0) break; digest.update(bytes,0,count) } }
  require(digest.digest().joinToString("") { "%02x".format(it.toInt() and 255) }==offer.hash) { "Update bytes changed" }
 }
 fun installIntent(file: File, offer: UpdateOffer): Intent {
  verify(file,offer)
  val uri=FileProvider.getUriForFile(context,"${context.packageName}.updates",file)
  return Intent(Intent.ACTION_VIEW).setDataAndType(uri,"application/vnd.android.package-archive").apply { clipData=ClipData.newRawUri("Agent update",uri); addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK) }
 }
 fun canInstall()=context.packageManager.canRequestPackageInstalls()
 fun permissionIntent()=Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,Uri.parse("package:${context.packageName}"))
}
data class UpdateState(val phase: String="Not checked yet",val offer: UpdateOffer?=null,val progress: Float=0f,val notes: List<Change>?=null,val notesStatus: String="",val error: String="")
class UpdateManager private constructor(
 private val scope: CoroutineScope,private val client: UpdateClient,private val target: File,
 private val installer: UpdateInstaller?,
 private val downloadFile: suspend (UpdateOffer,File,(Float)->Unit)->Unit,
 private val verifyFile: ((File,UpdateOffer)->Unit)?
) {
 constructor(context: Context,scope: CoroutineScope,client: UpdateClient=UpdateClient()) :
  this(scope,client,File(context.cacheDir,"updates/update.apk"),UpdateInstaller(context),client::download,null)
 internal constructor(scope: CoroutineScope,client: UpdateClient,target: File,
  downloadFile: suspend (UpdateOffer,File,(Float)->Unit)->Unit,verifyFile: (File,UpdateOffer)->Unit) :
  this(scope,client,target,null,downloadFile,verifyFile)
 private val mutable=MutableStateFlow(UpdateState()); val state=mutable.asStateFlow()
 private var job: Job?=null
 fun check() {
  if(job?.isCompleted==false || state.value.phase=="Ready to install") return
  job=scope.launch {
   mutable.value=UpdateState(phase="Checking…")
   try { val offer=client.latest(BuildConfig.VERSION_CODE); mutable.value=UpdateState(phase=if(offer==null) "Up to date" else "Update available",offer=offer) }
   catch(e: Exception) { if(e is CancellationException) throw e; mutable.value=UpdateState(phase="Update unavailable",error=e.message ?: "Try again") }
  }
 }
 fun loadNotes() {
  val offer=state.value.offer ?: return
  if(state.value.notesStatus=="Loading…" || state.value.notes != null) return
  scope.launch {
   mutable.update { it.copy(notesStatus="Loading…") }
   try { val notes=client.changes(BuildConfig.VERSION_CODE,BuildConfig.SOURCE_COMMIT,offer); mutable.update { if(it.offer==offer) it.copy(notes=notes,notesStatus="") else it } }
   catch(e: Exception) { if(e is CancellationException) throw e; mutable.update { if(it.offer==offer) it.copy(notesStatus="Changelog unavailable. Retry") else it } }
  }
 }
 fun download() {
  val offer=state.value.offer ?: return
  if(job?.isCompleted==false) return
  job=scope.launch {
   val attempt=currentCoroutineContext().job
   mutable.update { it.copy(phase="Downloading…",progress=0f,error="") }
   try {
    withContext(Dispatchers.IO) { target.parentFile!!.mkdirs(); target.delete() }
    downloadFile(offer,target) { progress -> if(attempt.isActive) mutable.update { it.copy(progress=progress) } }
    mutable.update { it.copy(phase="Verifying…") }
    withContext(Dispatchers.IO) { (verifyFile ?: requireNotNull(installer)::verify)(target,offer) }
    mutable.update { it.copy(phase="Ready to install") }
   } catch(e: Exception) {
    // The job remains incomplete (and all actions stay blocked) until every old
    // download finally and this cleanup have finished using the shared path.
    withContext(NonCancellable) {
     withContext(Dispatchers.IO) { target.delete() }
     mutable.update {
      if(e is CancellationException) it.copy(phase="Update available",progress=0f,error="Download cancelled")
      else it.copy(phase="Update failed",error=e.message ?: "Verification failed")
     }
    }
    if(e is CancellationException) throw e
   }
  }
 }
 fun install(launch: (Intent)->Unit) {
  if(state.value.phase!="Ready to install" || job?.isCompleted==false) return
  val offer=state.value.offer ?: return
  val installer=requireNotNull(installer)
  job=scope.launch {
   try {
    if(!installer.canInstall()) { launch(installer.permissionIntent()); return@launch }
    val intent=withContext(Dispatchers.IO) { installer.installIntent(target,offer) }
    launch(intent) // Only this explicit UI action can launch Android's confirmation flow.
   } catch(e: Exception) { if(e is CancellationException) throw e; mutable.update { it.copy(error="Installation could not start. Retry or download again.") } }
  }
 }
 fun cancelDownload() {
  if(state.value.phase !in setOf("Downloading…","Verifying…") || job?.isActive!=true) return
  job?.cancel()
  client.cancel()
  // Do not expose Download again or detach file deletion from the owning job.
 }
}
