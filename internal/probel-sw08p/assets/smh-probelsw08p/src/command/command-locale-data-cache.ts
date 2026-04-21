import { LocaleDataFileSystemService } from '../common/locale-data/locale-data-filesystem.service';
import { LocaleData } from '../common/locale-data/locale-data.model';
import { LocaleDataervice } from '../common/locale-data/locale-data.service';
import { LoggingService } from '../common/logging/logging.service';

export class LocaleDataCache {
    static readonly LOCALE_ROOT_DIR = `${__dirname}/../../locale-data`;
    static readonly RX_GENERAL_COMMAND_LOCALE_DATA = 'rx-general-command';
    static readonly TX_GENERAL_COMMAND_LOCALE_DATA = 'tx-general-command';

    static readonly RX_EXTENDED_COMMAND_LOCALE_DATA = 'rx-extended-command';
    static readonly TX_EXTENDED_COMMAND_LOCALE_DATA = 'tx-extended-command';

    static readonly ERROR_LOCALE_DATA = 'error';
    static readonly COMMAND_ERROR_LOCALE_DATA = 'command-error';
    static readonly _INSTANCE: LocaleDataCache = new LocaleDataCache();

    private _rxGeneralCommandLocaleDataervice: LocaleDataervice;
    private _txGeneralCommandLocaleDataervice: LocaleDataervice;

    private _rxExtendedCommandLocaleDataervice: LocaleDataervice;
    private _txExtendedCommandDataervice: LocaleDataervice;

    private _errorLocaleDataervice: LocaleDataervice;
    private _commandErrorLocaleDataervice: LocaleDataervice;

    static get INSTANCE(): LocaleDataCache {
        return LocaleDataCache._INSTANCE;
    }

    constructor() {
        const loggingService = new LoggingService();
        const localeDataFileSystemService = new LocaleDataFileSystemService(
            loggingService,
            LocaleDataCache.LOCALE_ROOT_DIR
        );

        this._rxGeneralCommandLocaleDataervice = new LocaleDataervice(loggingService, localeDataFileSystemService);
        this._txGeneralCommandLocaleDataervice = new LocaleDataervice(loggingService, localeDataFileSystemService);

        this._rxExtendedCommandLocaleDataervice = new LocaleDataervice(loggingService, localeDataFileSystemService);
        this._txExtendedCommandDataervice = new LocaleDataervice(loggingService, localeDataFileSystemService);

        this._errorLocaleDataervice = new LocaleDataervice(loggingService, localeDataFileSystemService);
        this._commandErrorLocaleDataervice = new LocaleDataervice(loggingService, localeDataFileSystemService);
    }

    loadLocaleDataAsync(localeId: string): Promise<void> {
        return Promise.all([
            this._rxGeneralCommandLocaleDataervice.loadLocaleDataAsync(
                LocaleDataCache.RX_GENERAL_COMMAND_LOCALE_DATA,
                localeId
            ),
            this._txGeneralCommandLocaleDataervice.loadLocaleDataAsync(
                LocaleDataCache.TX_GENERAL_COMMAND_LOCALE_DATA,
                localeId
            ),
            this._rxExtendedCommandLocaleDataervice.loadLocaleDataAsync(
                LocaleDataCache.RX_EXTENDED_COMMAND_LOCALE_DATA,
                localeId
            ),
            this._txExtendedCommandDataervice.loadLocaleDataAsync(
                LocaleDataCache.TX_EXTENDED_COMMAND_LOCALE_DATA,
                localeId
            ),
            this._errorLocaleDataervice.loadLocaleDataAsync(LocaleDataCache.ERROR_LOCALE_DATA, localeId),
            this._commandErrorLocaleDataervice.loadLocaleDataAsync(LocaleDataCache.COMMAND_ERROR_LOCALE_DATA, localeId)
        ]).then(() => Promise.resolve());
    }

    getRxGeneralCommandLocalData(localeDataKey: string): LocaleData {
        return this._rxGeneralCommandLocaleDataervice.getLocalData(localeDataKey);
    }

    getTxGeneralCommandLocaleData(localeDataKey: string): LocaleData {
        return this._txGeneralCommandLocaleDataervice.getLocalData(localeDataKey);
    }

    getRxExtendedCommandLocaleData(localeDataKey: string): LocaleData {
        return this._rxExtendedCommandLocaleDataervice.getLocalData(localeDataKey);
    }

    getTxExtendedCommandData(localeDataKey: string): LocaleData {
        return this._txExtendedCommandDataervice.getLocalData(localeDataKey);
    }

    getErrorLocaleData(localeDataKey: string): LocaleData {
        return this._errorLocaleDataervice.getLocalData(localeDataKey);
    }

    getCommandErrorLocaleData(localeDataKey: string): LocaleData {
        return this._commandErrorLocaleDataervice.getLocalData(localeDataKey);
    }
}
