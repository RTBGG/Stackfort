// SPDX-License-Identifier: AGPL-3.0-or-later

import { createApp } from 'vue'
import App from './App.vue'
import { i18n } from './i18n'
import './styles.css'

createApp(App).use(i18n).mount('#app')
