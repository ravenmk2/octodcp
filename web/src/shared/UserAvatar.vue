<script setup lang="ts">
import UiIcon from "./UiIcon.vue";
import type { User } from "../features/identity/store";
defineProps<{ user: User; large?: boolean }>();
</script>
<template>
  <span class="avatar" :class="{ founder: user.is_founder, large }"
    ><img v-if="user.avatar" :src="user.avatar.url" alt="" /><UiIcon
      v-else
      name="user"
  /></span>
</template>
