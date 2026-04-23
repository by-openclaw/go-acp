import { LocaleDataCache } from './command/command-locale-data-cache';

export class BootstrapService {
    static bootstrapAsync(localeId = 'en'): Promise<void> {
        // Initialize the Locale Data caches for a specific LocaleId
        return LocaleDataCache.INSTANCE.loadLocaleDataAsync(localeId);
    }
}
