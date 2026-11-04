'use client';

import { useMemo } from 'react';
import { useSellerSession } from '@/features/auth/session';
import type { NavItem, NavGroup } from '@/types';

export function useFilteredNavItems(items: NavItem[]) {
  const { role, workspaceId } = useSellerSession();
  return useMemo(() => {
    function visible(item: NavItem) {
      const access = item.access;
      if (!access) return true;
      if (access.requireOrg && !workspaceId) return false;
      if ((access.permission || access.role) && role !== 'owner') return false;
      return true;
    }
    return items.filter(visible).map((item) => ({
      ...item,
      items: item.items?.filter(visible) ?? []
    }));
  }, [items, role, workspaceId]);
}

export function useFilteredNavGroups(groups: NavGroup[]) {
  const allItems = useMemo(() => groups.flatMap((g) => g.items), [groups]);
  const filteredItems = useFilteredNavItems(allItems);

  return useMemo(() => {
    const filteredSet = new Set(filteredItems.map((item) => item.title));
    return groups
      .map((group) => ({
        ...group,
        items: filteredItems.filter((item) =>
          group.items.some((gi) => gi.title === item.title && filteredSet.has(gi.title))
        )
      }))
      .filter((group) => group.items.length > 0);
  }, [groups, filteredItems]);
}
