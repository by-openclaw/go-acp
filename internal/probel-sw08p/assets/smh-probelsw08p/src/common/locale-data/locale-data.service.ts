import { Service } from 'typedi';

import { LoggingService } from '../logging/logging.service';
import { LocaleDataFileSystemService, LocaleDataList } from './locale-data-filesystem.service';
import { LocaleDataError } from './locale-data.error';
import { LocaleData } from './locale-data.model';

/**
 * Supports localization of resources.
 *
 * @export
 * @class LocalDataervice
 */
@Service()
export class LocaleDataervice {
    /**
     * LocaleData description returned as fallback when LocaleData key is not found
     *
     * @static
     * @memberof LocaleDataervice
     */
    static readonly FALL_BACK_DESCRIPTION = '#####';

    private localeDataMap: Map<string, LocaleDataList>;
    private _localeDataType?: string;
    private _localeId?: string;

    /**
     * Creates an instance of LocaleDataervice
     *
     * @param {LoggingService} _loggingService the logging service
     * @param {LocaleDataFileSystemService} _localeDataFileSystemService the service providing access to Locale Data file system
     * @memberof LocaleDataervice
     */
    constructor(
        private _loggingService: LoggingService,
        private _localeDataFileSystemService: LocaleDataFileSystemService
    ) {
        this.localeDataMap = new Map<string, LocaleDataList>();
    }

    /**
     * Loads asynchronously a Locale Data file based on its type and an optional Locale Id
     * If an error occurs, a 'LocaleDataError' is thrown
     *
     * @param {string} localDataType the Locale Data Type - E.G : 'command-error', 'general-error', ...
     * @param {string} [localeId='en'] the Locale Id
     * @returns {Promise<this>}
     * @memberof LocaleDataervice
     */
    async loadLocaleDataAsync(localeDataType: string, localeId = 'en'): Promise<this> {
        try {
            const localeDataList: LocaleDataList = await this._localeDataFileSystemService.loadLocaleDataFileAsync(
                localeDataType,
                localeId
            );
            this.localeDataMap.set(localeDataType, localeDataList);

            this._localeDataType = localeDataType;
            this._localeId = localeId;

            return this;
        } catch (err) {
            throw new LocaleDataError(
                `Locale Data cannot be loaded due to [${err}] ! \n` +
                    `${{ localeDataType, localeId, rootDir: this._localeDataFileSystemService.rootDir }}`,
                err
            );
        }
    }

    /**
     * Gets a localized data by specifying a LocaleData key that uniquely identifies a Locale Data cross Locale Data files
     *
     * @param {string} localeDataKey the LocaleData key
     * @param {boolean} [useFallBack=false] Boolean that indicates if an fallback description must be returned or en error thrown
     * if the LocaleData key is found in the LocaleData file of the LocaleId
     * @returns {LocaleData}
     * @memberof LocaleDataervice
     */
    getLocalData(localeDataKey: string, useFallBack = false): LocaleData {
        if (!this._localeDataType || !this.localeDataMap.has(this._localeDataType)) {
            throw new LocaleDataError(
                `Locale data type not yet specified ! Please use the loadLocaleDataAsync(...) method.`
            );
        }

        const localeDataList = this.localeDataMap.get(this._localeDataType) as LocaleDataList;
        if (!localeDataList.has(localeDataKey)) {
            return this.getAndLogFallBackLoaleData(localeDataKey);
        }

        return localeDataList.get(localeDataKey) as LocaleData;
    }

    private getAndLogFallBackLoaleData(localeDataKey: string): LocaleData {
        const fallbackLocaleData: LocaleData = {
            id: localeDataKey,
            description: LocaleDataervice.FALL_BACK_DESCRIPTION
        };
        this._loggingService.warn(
            () =>
                `Local key [${localeDataKey}] not found [ LocaleDataType:${this._localeDataType} - LocaleId:${this._localeId}] ! (FallBack LocaleData used\n`,
            fallbackLocaleData
        );
        return fallbackLocaleData;
    }
}
