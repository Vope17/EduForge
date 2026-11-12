import { createRouter, createWebHistory } from 'vue-router'
import HomeView from './views/HomeView.vue'

export const routes = [
  { path: '/', name: 'home', component: HomeView },
  { path: '/settings/models', name: 'model-settings', component: () => import('./views/ModelSettingsView.vue') },
  { path: '/slides', name: 'slides', component: () => import('./views/SlidesView.vue') },
  { path: '/slides/:id', name: 'slide-deck', component: () => import('./views/SlideDeckView.vue') },
  { path: '/documents', name: 'documents', component: () => import('./views/DocumentsView.vue') },
  { path: '/documents/:id', name: 'document', component: () => import('./views/DocumentView.vue') },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior: (to) => to.hash === '#formats' ? { el: '#formats' } : { top: 0 },
})

