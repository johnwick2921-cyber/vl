import { traderApi } from './traders'
import { strategyApi } from './strategies'
import { configApi } from './config'
import { dataApi } from './data'
import { telegramApi } from './telegram'
import { planApi } from './plan'
import { friendsApi } from './friends'

export const api = {
  ...traderApi,
  ...strategyApi,
  ...configApi,
  ...dataApi,
  ...telegramApi,
  ...planApi,
  ...friendsApi,
}
