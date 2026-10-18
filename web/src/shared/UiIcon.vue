<script setup lang="ts">
defineProps<{
  name:
    | "arrow"
    | "back"
    | "check"
    | "database"
    | "shield"
    | "user"
    | "refresh"
    | "home"
    | "dashboard"
    | "users"
    | "settings"
    | "chevron"
    | "key"
    | "globe"
    | "more"
    | "logout"
    | "panel"
    | "expand"
    | "menu"
    | "plus"
    | "search"
    | "log"
    | "lock"
    | "block"
    | "device";
}>();
const paths = {
  device: "M3 4h18v12H3V4Zm5 16h8m-4-4v4",
  key: "M14 8a5 5 0 1 1-10 0 5 5 0 0 1 10 0Zm-2 4 9 9m-5-5 3-3m-1 5 3-3",
  globe:
    "M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0ZM3 12h18M12 3a18 18 0 0 1 0 18 18 18 0 0 1 0-18Z",
  more: "M5 12h.01M12 12h.01M19 12h.01",
  logout: "M9 4H4v16h5m6-14 6 6-6 6m-7-6h13",
  panel: "M3 4h18v16H3V4Zm5 0v16m8-12-4 4 4 4",
  expand: "M3 4h18v16H3V4Zm5 0v16m5-12 4 4-4 4",
  menu: "M4 6h16M4 12h16M4 18h16",
  plus: "M12 5v14M5 12h14",
  search: "M19 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0Zm-2 5 5 6",
  log: "M5 3h14v18H5V3Zm4 5h6m-6 4h6m-6 4h4",
  lock: "M6 10h12v11H6V10Zm3 0V6a3 3 0 0 1 6 0v4m-3 5v2",
  block: "M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0ZM6 6l12 12",
  dashboard: "M3 3h7v7H3V3Zm11 0h7v7h-7V3ZM3 14h7v7H3v-7Zm11 0h7v7h-7v-7Z",
  users:
    "M15 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0ZM3 21v-2a7 7 0 0 1 14 0v2M17 4a4 4 0 0 1 0 7m2 4a6 6 0 0 1 3 5v1",
  settings:
    "M9 3h6l1 3 3 1 2 5-2 5-3 1-1 3H9l-1-3-3-1-2-5 2-5 3-1 1-3Zm7 9a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z",
  chevron: "m9 6 6 6-6 6",
  home: "m3 10 9-7 9 7v10H3V10Zm6 10v-7h6v7",
  arrow: "M5 12h14m-6-6 6 6-6 6",
  back: "M19 12H5m6-6-6 6 6 6",
  check: "m5 12 4 4L19 6",
  database:
    "M20 6c0 2.2-16 2.2-16 0s16-2.2 16 0ZM4 6v12c0 3 16 3 16 0V6M4 12c0 3 16 3 16 0",
  shield: "m12 3 8 4v5c0 5-8 9-8 9S4 17 4 12V7l8-4Zm-4 9 3 3 5-6",
  user: "M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0ZM4 21v-2a8 8 0 0 1 16 0v2",
  refresh:
    "M20 7v5h-5M4 17v-5h5M5.5 7a8 8 0 0 1 13-2L20 8M4 16l1.5 3a8 8 0 0 0 13-2",
};
</script>

<template>
  <svg
    class="ui-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.7"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path :d="paths[name]" />
  </svg>
</template>
