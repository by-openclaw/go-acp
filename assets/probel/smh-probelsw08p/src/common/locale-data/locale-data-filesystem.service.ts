import * as fs from 'mz/fs';
import { Service } from 'typedi';

import { LoggingService } from '../logging/logging.service';
import { JsonUtility } from '../utility/json.utility';
import { LocaleData } from './locale-data.model';

export type LocaleDataList = Map<string, LocaleData>;

/**
 * Loads Locale Data from Locale Data file according to conventions.
 * Locale Data file :
 * - resides under the 'rootDir' folder
 * - respects the format : rootDir/localDataType-localeId.json
 * E.G : `./local-data/command-errors/en.json
 *
 * @export
 * @class LocaleDataFileSystemService
 */
@Service()
export class LocaleDataFileSystemService {
    /**
     * Root folder where Locale Data JSON files are maintained
     *
     * @static
     * @memberof LocaleDataFileSystemService
     */
    static readonly LOCALE_DATA_FOLDER = 'locale-data';

    /**
     * Default locale data encoding
     *
     * @static
     * @memberof LocaleDataFileSystemService
     */
    static readonly LOCALE_DATA_ENCODING = 'utf-8';

    /**
     * Creates an instance of LocaleDataFileSystemService
     *
     * @param {LoggingService} loggingService the loggingService
     * @param {string} _rootDir the Locale Data root folder
     * @memberof LocaleDataFileSystemService
     */
    constructor(private loggingService: LoggingService, private _rootDir: string) {}

    /**
     * Gets the Root folder where Locale Data files are maintained
     *
     * @readonly
     * @type {string}
     * @memberof LocaleDataFileSystemService
     */
    get rootDir(): string {
        return this._rootDir;
    }

    /**
     * Returns the Locale Data file name based on a locale Data type, a locale id and a root folder
     *
     * @private
     * @static
     * @param {string} localeDataType the Locale Data Type - E.G : 'command-error', 'general-error', ...
     * @param {string} localeId the Locale Id - E.G : en, fr, ...
     * @param {string} rootDir the root folder where Locale Data file are maintained
     * @returns {string}
     * @memberof LocaleDataFileSystemService
     */
    private static getFileNameForLocale(localeDataType: string, localeId: string, rootDir: string): string {
        const localeDataFileName = `${localeDataType}-${localeId}.json`;
        return `${rootDir}/${localeDataFileName.toLowerCase()}`;
    }

    /**
     * Load asynchronously a Locale Data file from the 'rootDir' folder
     *
     * @param {string} localeDataType the Locale Data Type - E.G : 'command-error', 'general-error', ...
     * @param {string} localeId the Locale Id - E.G : en, fr, ...
     * @returns {Promise<LocaleDataList>}
     * @memberof LocaleDataFileSystemService
     */
    async loadLocaleDataFileAsync(localeDataType: string, localeId: string): Promise<LocaleDataList> {
        const localeDataFileName = LocaleDataFileSystemService.getFileNameForLocale(
            localeDataType,
            localeId,
            this._rootDir
        );
        const fileContent = await fs.readFile(localeDataFileName, {
            encoding: LocaleDataFileSystemService.LOCALE_DATA_ENCODING
        });
        const localeDataArray = JsonUtility.safeJSONParse(fileContent).data;
        const localeDataList = new Map<string, LocaleData>();
        localeDataArray.forEach((data: LocaleData) => {
            localeDataList.set(data.id, data);
        });
        return localeDataList;
    }
}
