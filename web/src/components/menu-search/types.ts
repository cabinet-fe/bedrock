import type { Component } from "vue";

export interface SearchMenuItem {
  id: string;
  title: string;
  path: string;
  groupTitle: string;
  icon: Component | string;
  keywords: string[];
}

export interface SearchMenuGroup {
  title: string;
  maxScore?: number;
  items: SearchMenuItem[];
}

export interface HighlightPart {
  text: string;
  highlight: boolean;
}
