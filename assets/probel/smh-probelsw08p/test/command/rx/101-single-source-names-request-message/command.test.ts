import { SingleSourceNamesRequestMessageCommand } from '../../../../src/command/rx/101-single-source-names-request-message/command';
import { SingleSourceNamesRequestMessageCommandParams } from '../../../../src/command/rx/101-single-source-names-request-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { SingleSourceNamesRequestMessageCommandOptions } from '../../../../src/command/rx/101-single-source-names-request-message/options';
import { LengthOfNamesRequired } from '../../../../src/command/shared/length-of-names-required';

describe('Single Source Names Request Message Command)', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: SingleSourceNamesRequestMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                sourceId: 0
            };
            const options: SingleSourceNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new SingleSourceNamesRequestMessageCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: SingleSourceNamesRequestMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                sourceId: -1
            };

            const options: SingleSourceNamesRequestMessageCommandOptions = {
                lengthOfNames: -1
            };
            // Act
            const fct = () => new SingleSourceNamesRequestMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: SingleSourceNamesRequestMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                sourceId: 65536
            };

            const options: SingleSourceNamesRequestMessageCommandOptions = {
                lengthOfNames: 10
            };
            // Act
            const fct = () => new SingleSourceNamesRequestMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Single Source Name Request Message CMD_101_0X65', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.SINGLE_SOURCE_NAME_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleSourceNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    sourceId: 0
                };

                const options: SingleSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '65 00 00 00 00', // data
                    5, // bytesCount
                    0x96, // checksum
                    '10 02 65 00 00 00 00 05 96 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleSourceNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    sourceId: 0
                };

                const options: SingleSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '65 00 01 00 00', // data
                    5, // bytesCount
                    0x95, // checksum
                    '10 02 65 00 01 00 00 05 95 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleSourceNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    sourceId: 0
                };

                const options: SingleSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '65 00 02 00 00', // data
                    5, // bytesCount
                    0x94, // checksum
                    '10 02 65 00 02 00 00 05 94 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleSourceNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    sourceId: 0
                };

                const options: SingleSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '65 00 02 00 00', // data
                    5, // bytesCount
                    0x94, // checksum
                    '10 02 65 00 02 00 00 05 94 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleSourceNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    sourceId: 896
                };

                const options: SingleSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.SIXTEEN_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '65 00 03 03 80', // data
                    5, // bytesCount
                    0x10, // checksum
                    '10 02 65 00 03 03 80 05 10 10 10 03' // buffer
                );
            });
        });

        describe('Extended General Single Source Name Request Message CMD_229_0Xe5', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.SINGLE_SOURCE_NAMES_REQUEST_MESSAGE;
            });
            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: SingleSourceNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    sourceId: 0
                };

                const options: SingleSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E5 10 00 00 00 00', // data
                    6, // bytesCount
                    0x05, // checksum
                    '10 02 E5 10 10 00 00 00 00 06 05 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: SingleSourceNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    sourceId: 0
                };

                const options: SingleSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E5 10 00 01 00 00', // data
                    6, // bytesCount
                    0x04, // checksum
                    '10 02 E5 10 10 00 01 00 00 06 04 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: SingleSourceNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    sourceId: 0
                };

                const options: SingleSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E5 10 00 02 00 00', // data
                    6, // bytesCount
                    0x03, // checksum
                    '10 02 E5 10 10 00 02 00 00 06 03 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: SingleSourceNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    sourceId: 0
                };

                const options: SingleSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.SIXTEEN_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E5 10 00 03 00 00', // data
                    6, // bytesCount
                    0x02, // checksum
                    '10 02 E5 10 10 00 03 00 00 06 02 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: SingleSourceNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    levelId: 16,
                    sourceId: 65535
                };

                const options: SingleSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E5 00 10 00 FF FF', // data
                    6, // bytesCount
                    0x07, // checksum
                    '10 02 E5 00 10 10 00 FF FF 06 07 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.SINGLE_SOURCE_NAMES_REQUEST_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: SingleSourceNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    sourceId: 16
                };

                const options: SingleSourceNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new SingleSourceNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E5 10 10 00 00 10', // data
                    6, // bytesCount
                    0xe5, // checksum
                    '10 02 E5 10 10 10 10 00 00 10 10 06 E5 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: SingleSourceNamesRequestMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                sourceId: 0
            };

            const options: SingleSourceNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new SingleSourceNamesRequestMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log Extended General command description', () => {
            // Arrange
            const params: SingleSourceNamesRequestMessageCommandParams = {
                matrixId: 16,
                levelId: 16,
                sourceId: 896
            };

            const options: SingleSourceNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.SIXTEEN_CHAR_NAMES
            };
            // Act
            const command = new SingleSourceNamesRequestMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
