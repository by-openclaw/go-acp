import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { ProtectDetails } from './options';
import { ProtectTallyDumpCommandParams } from './params';

/**
 * Protect Tally Dump Message command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<ProtectTallyDumpCommandParams>}
 */
export class CommandParamsValidator implements IValidator<ProtectTallyDumpCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator.
     * @param {ProtectTallyDumpCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: ProtectTallyDumpCommandParams) {}

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
        if (this.isNumberOfProtectTalliesOutOfRange()) {
            errors.numberOfProtectTallies = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.NUMBER_OF_PROTECT_TALLIES_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isDestinationIdOutOfRange()) {
            errors.destinationId = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isDeviceNumberProtectDataItemsOutOfRange()) {
            errors.deviceNumberProtectDataItems = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.DEVICE_NUMBER_AND_PROTECT_DETAILS_ARE_OUT_OF_RANGE_ERROR_MSG
            );
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the matrixId is out of range
     * + false = matrixId is included within the limits
     * + true = matrixId [0-255] is out of range
     * @private
     * @returns {boolean}
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
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isLevelIdOutOfRange(): boolean {
        return this.data.levelId < 0 || this.data.levelId > 255;
    }

    /**
     * Gets a boolean indicating whether the numberOfProtectTallies is out of range
     * + false = levelId is numberOfProtectTallies within the limits
     * + true = numberOfProtectTallies [0-63] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isNumberOfProtectTalliesOutOfRange(): boolean {
        return this.data.numberOfProtectTallies < 1 || this.data.numberOfProtectTallies > 64;
    }

    /**
     * Gets a boolean indicating whether the destinationId is out of range
     * + false = destinationId is included within the limits
     * + true = destinationId  [0-65535] is out of range
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isDestinationIdOutOfRange(): boolean {
        return this.data.firstDestinationId < 0 || this.data.firstDestinationId > 65535;
    }

    // TODO: To refactor
    /**
     * Gets a boolean indicating whether the deviceId or/and protectedData are out of range
     *
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isDeviceNumberProtectDataItemsOutOfRange(): boolean {
        if (this.data.deviceNumberProtectDataItems.length === 0) {
            return true;
        }
        for (let byteIndex = 0; byteIndex < this.data.deviceNumberProtectDataItems.length; byteIndex++) {
            const data = this.data.deviceNumberProtectDataItems[byteIndex];
            if (data.deviceId < 0 || data.deviceId > 1023) {
                return true;
            }
            if (
                data.protectedData < ProtectDetails.NOT_PROTECTED ||
                data.protectedData > ProtectDetails.OEM_PROTECTED
            ) {
                return true;
            }
        }
        return false;
    }
}
