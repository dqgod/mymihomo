import { latencyMapOf, smartOrderMap, type LatencyMap } from '@/assembly/proxies'
import { NOT_CONNECTED, PROXY_SORT_TYPE } from '@/constant'
import { isProxyGroup } from '@/helper'
import {
  isProxyNodeSearchMode,
  matchProxySearchKeyword,
  proxySearchKeyword,
} from '@/helper/proxy-search'
import {
  hideUnavailableProxies,
  proxyGroupFilterMap,
  proxyGroupHideUnavailableMap,
  proxyGroupSortMap,
  proxySortType,
  useSmartGroupSort,
} from '@/store/settings'
import { computed, type ComputedRef } from 'vue'

export function useRenderProxyList(proxies: ComputedRef<string[]>, groupName?: string) {
  const latencyMap = latencyMapOf(groupName)

  const renderProxies = computed(() => {
    const filtered = filterProxies(proxies.value, groupName, latencyMap.value)

    return sortProxies(filtered, groupName, latencyMap.value)
  })

  const proxiesCount = computed(() => {
    const latencies = latencyMap.value
    let available = 0

    for (const proxy of renderProxies.value) {
      if ((latencies.get(proxy) ?? NOT_CONNECTED) !== NOT_CONNECTED) {
        available++
      }
    }

    return `${available}/${proxies.value.length}`
  })

  return { renderProxies, proxiesCount }
}

const filterProxies = (
  proxies: string[],
  groupName: string | undefined,
  latencyMap: LatencyMap,
) => {
  let result = proxies

  if (
    (groupName ? proxyGroupHideUnavailableMap.value[groupName] : undefined) ??
    hideUnavailableProxies.value
  ) {
    result = result.filter(
      (name) => isProxyGroup(name) || (latencyMap.get(name) ?? NOT_CONNECTED) > NOT_CONNECTED,
    )
  }

  if (isProxyNodeSearchMode.value && proxySearchKeyword.value) {
    const keyword = proxySearchKeyword.value
    result = result.filter((name) => matchProxySearchKeyword(name, keyword))
  }

  const groupKeyword = groupName ? proxyGroupFilterMap.value[groupName] : ''
  if (groupKeyword) {
    result = result.filter((name) => matchProxySearchKeyword(name, groupKeyword))
  }

  return result
}

const sortProxies = (proxies: string[], groupName: string | undefined, latencyMap: LatencyMap) => {
  const override = groupName ? proxyGroupSortMap.value[groupName] : undefined
  const sortType = override ?? proxySortType.value
  if (!override && groupName && useSmartGroupSort.value && smartOrderMap.value[groupName]) {
    return sortBySmartOrder(proxies, smartOrderMap.value[groupName])
  }

  if (sortType === PROXY_SORT_TYPE.DEFAULT) {
    return proxies
  }

  const groups: string[] = []
  const nodes: string[] = []
  proxies.forEach((proxy) => {
    ;(isProxyGroup(proxy) ? groups : nodes).push(proxy)
  })

  const sortFunc = getSortFunc(sortType, latencyMap)
  return groups.concat(nodes.sort(sortFunc))
}

const sortBySmartOrder = (proxies: string[], orderMap: Record<string, number>) => {
  return [...proxies].sort((a, b) => {
    const ia = orderMap[a] ?? Number.MAX_SAFE_INTEGER
    const ib = orderMap[b] ?? Number.MAX_SAFE_INTEGER
    return ia - ib
  })
}

const getSortFunc = (sortType: PROXY_SORT_TYPE, latencyMap: LatencyMap) => {
  const latencyFor = (name: string) => {
    const latency = latencyMap.get(name) ?? NOT_CONNECTED

    return latency === NOT_CONNECTED ? Infinity : latency
  }
  switch (sortType) {
    case PROXY_SORT_TYPE.NAME_ASC:
      return (a: string, b: string) => a.localeCompare(b)
    case PROXY_SORT_TYPE.NAME_DESC:
      return (a: string, b: string) => b.localeCompare(a)
    case PROXY_SORT_TYPE.LATENCY_ASC:
      return (a: string, b: string) => latencyFor(a) - latencyFor(b)
    case PROXY_SORT_TYPE.LATENCY_DESC:
      return (a: string, b: string) => {
        const x = latencyFor(a),
          y = latencyFor(b)
        if (x === Infinity) return y === Infinity ? 0 : 1
        if (y === Infinity) return -1
        return y - x
      }
    default:
      return undefined
  }
}
