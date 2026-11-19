plugins { id("com.android.application"); id("org.jetbrains.kotlin.android"); id("org.jetbrains.kotlin.plugin.compose") }
fun gitText(vararg args: String): String = providers.exec { commandLine("git", *args); workingDir(rootProject.rootDir) }.standardOutput.asText.get().trim()
val countText = providers.gradleProperty("agentCommitCount").orNull ?: run {
 require(gitText("rev-parse", "--is-shallow-repository") == "false") { "Fetch full history" }
 gitText("rev-list", "--count", "HEAD")
}
require(Regex("[1-9][0-9]*").matches(countText)) { "agentCommitCount must be a positive integer" }
val count = requireNotNull(countText.toIntOrNull()?.takeIf { it <= 2100000000 })
val keys = listOf("AGENT_KEYSTORE_PATH", "AGENT_KEYSTORE_PASSWORD", "AGENT_KEY_ALIAS", "AGENT_KEY_PASSWORD").map { providers.environmentVariable(it).orNull }
require(keys.all { it.isNullOrEmpty() } || keys.all { !it.isNullOrEmpty() }) { "Supply all four AGENT signing variables" }
android {
 namespace = "com.everyonesoftware.agent"; compileSdk = 36; buildToolsVersion = "36.0.0"
 defaultConfig {
  applicationId = "com.everyonesoftware.agent"; minSdk = 26; targetSdk = 36
  versionCode = count; versionName = "0.1.$count"
  buildConfigField("String", "SOURCE_COMMIT", "\"${gitText("rev-parse", "HEAD")}\"")
  testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
 }
 buildFeatures { compose = true; buildConfig = true }
 compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
 kotlinOptions { jvmTarget = "17" }
 signingConfigs { if (keys.all { !it.isNullOrEmpty() }) create("release") { storeFile = file(keys[0]!!); storePassword = keys[1]; keyAlias = keys[2]; keyPassword = keys[3] } }
 buildTypes { release { if (keys.all { !it.isNullOrEmpty() }) signingConfig = signingConfigs.getByName("release"); isMinifyEnabled = false } }
 packaging { resources.excludes += "/META-INF/{AL2.0,LGPL2.1}" }
}
val validateRelease by tasks.registering { doLast {
 check(keys.all { !it.isNullOrEmpty() }) { "Dedicated AGENT release signing required" }
 check(file(keys[0]!!).isFile) { "Missing keystore" }
 check(gitText("rev-parse", "--is-shallow-repository") == "false")
 check(gitText("status", "--porcelain").isEmpty()) { "Publish only committed clean trees" }
 check(gitText("rev-list", "--count", "HEAD") == countText)
} }
tasks.matching { it.name == "preReleaseBuild" }.configureEach { dependsOn(validateRelease) }
dependencies {
 val bom = platform("androidx.compose:compose-bom:2025.11.01")
 implementation(bom); androidTestImplementation(bom)
 implementation("androidx.core:core-ktx:1.17.0")
 implementation("androidx.activity:activity-compose:1.11.0")
 implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.9.4")
 implementation("androidx.lifecycle:lifecycle-runtime-compose:2.9.4")
 implementation("androidx.compose.ui:ui"); implementation("androidx.compose.material3:material3")
 implementation("androidx.compose.material:material-icons-extended")
 implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")
 implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.9.0")
 implementation("com.android.tools.build:apksig:8.13.2")
 implementation("com.squareup.okhttp3:okhttp:4.12.0")
 testImplementation("junit:junit:4.13.2"); testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
 testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.10.2")
 androidTestImplementation("androidx.test.ext:junit:1.3.0"); androidTestImplementation("androidx.test:runner:1.7.0")
 androidTestImplementation("androidx.test:rules:1.7.0"); androidTestImplementation("androidx.compose.ui:ui-test-junit4")
 androidTestImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
 debugImplementation("androidx.compose.ui:ui-test-manifest")
}
