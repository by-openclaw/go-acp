import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { SourceNamesResponseCommandOptions } from './options';
import { SourceNamesResponseCommandParams } from './params';

/**
 * Source Names Response Message command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<SourceNamesResponseCommandParams>}
 */
export class CommandParamsValidator implements IValidator<SourceNamesResponseCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {SourceNamesResponseCommandParams} data the command parameters
     * @param {SourceNamesResponseCommandOptions} _options the command options (internally used)
     * @memberof CommandParamsValidator
     */
    constructor(
        readonly data: SourceNamesResponseCommandParams,
        private readonly _options: SourceNamesResponseCommandOptions
    ) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isMatrixIdOutOfRange()) {
            errors.matrixId = cache.getCommandErrorLocaleData(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isLevelIdOutOfRange()) {
            errors.levelId = cache.getCommandErrorLocaleData(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isfirstSourceIdOutOfRange()) {
            errors.firstSourceId = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.FIRST_NAME_NUMBER_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isSourceIdAndnumberOfSourceNamesToFollowOutOfRange()) {
            errors.sourceIdAndMaximumNumberOfNames = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isSourceNamesItemsOutOfRange()) {
            errors.sourceNamesItems = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the matrixId is out of range
     * + false = matrixId is included within the limits
     * + true = matrixId [0-255] is out of range
     * @private
     * @returns {boolean} indicating if matrixId is out of range
     * @memberof CommandParamsValidator
     */
    private isMatrixIdOutOfRange(): boolean {
        return this.data.matrixId < 0 || this.data.matrixId > 255;
    }

    /**
     * Gets a boolean indicating whether the levelId is out of range
     * + false = levelId is included within the limits
     * + true = levelId [0-255] is out of range
     * @private
     * @returns {boolean} indicating if levelId is out of range
     * @memberof CommandParamsValidator
     */
    private isLevelIdOutOfRange(): boolean {
        return this.data.levelId < 0 || this.data.levelId > 255;
    }

    /**
     * Gets a boolean indicating whether the firstSourceId is out of range
     * + false = firstSourceId is included within the limits
     * + true = firstSourceId [0-65535] is out of range
     * @private
     * @returns {boolean} indicating if levelId is out of range
     * @memberof CommandParamsValidator
     */
    private isfirstSourceIdOutOfRange(): boolean {
        return this.data.firstSourceId < 0 || this.data.firstSourceId > 65535;
    }

    /**
     * Gets a boolean indicating whether the sourceId + number of source Names to follow is out of range
     * + false = sourceId is included within the limits
     * + true = sourceId  [0-65535] is out of range
     * @private
     * @returns {boolean} indicating if sourceId + number of source Names to follow is out of range
     * @memberof CommandParamsValidator
     */
    private isSourceIdAndnumberOfSourceNamesToFollowOutOfRange(): boolean {
        return (
            this.data.firstSourceId + this.data.numberOfSourceNamesToFollow < 1 ||
            this.data.firstSourceId + this.data.numberOfSourceNamesToFollow > 65535
        );
    }

    /**
     * Gets a boolean indicating whether the sourceNameItems is out of range
     * + false = sourceNameItems is included within the limits
     * + true = sourceNameItems  [depend of the lengthOfSourceNamesReturned.byteLength] is out of range
     * @private
     * @returns {boolean} indicating if Source Names Items length is out of range
     * @memberof CommandParamsValidator
     */
    private isSourceNamesItemsOutOfRange(): boolean {
        if (this.data.sourceNameItems.length === 0) {
            return true;
        }
        for (let byteIndex = 0; byteIndex < this.data.sourceNameItems.length; byteIndex++) {
            const data = this.data.sourceNameItems[byteIndex];
            if (data.length < 1 || data.length > this._options.lengthOfSourceNamesReturned.byteLength) {
                return true;
            }
        }
        return false;
    }
}
